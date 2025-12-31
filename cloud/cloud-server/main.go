package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

// ============================================================================
// CONFIGURATION
// ============================================================================

type Config struct {
	RedisAddr  string
	MQTTBroker string
	HTTPPort   string
}

func loadConfig() *Config {
	return &Config{
		RedisAddr:  "localhost:6379",
		MQTTBroker: "tcp://localhost:1883",
		HTTPPort:   ":8080",
	}
}

// ============================================================================
// REDIS CLIENT
// ============================================================================

var ctx = context.Background()

type RedisClient struct {
	client *redis.Client
}

func newRedisClient(addr string) *RedisClient {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   0,
	})

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}

	log.Println("✅ Connected to Redis")
	return &RedisClient{client: rdb}
}

// Store latest sensor reading with full metadata
func (r *RedisClient) storeSensorReading(sensorID int, sensorType string, value float64, unit string, lat, lon float64, timestamp int64) error {
	key := fmt.Sprintf("sensor:%d:latest", sensorID)
	data := map[string]interface{}{
		"sensor_id": sensorID,
		"type":      sensorType,
		"value":     value,
		"unit":      unit,
		"lat":       lat,
		"lon":       lon,
		"timestamp": timestamp,
	}

	// Add to sensors set for easy listing
	r.client.SAdd(ctx, "sensors", sensorID)

	return r.client.HSet(ctx, key, data).Err()
}

// Store reading in history (keep last 1000)
func (r *RedisClient) storeSensorHistory(sensorID int, value float64, timestamp int64) error {
	key := fmt.Sprintf("sensor:%d:history", sensorID)
	data := fmt.Sprintf(`{"value":%.2f,"timestamp":%d}`, value, timestamp)

	pipe := r.client.Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, 999)
	_, err := pipe.Exec(ctx)
	return err
}

// Get latest reading
func (r *RedisClient) getLatestReading(sensorID int) (map[string]string, error) {
	key := fmt.Sprintf("sensor:%d:latest", sensorID)
	return r.client.HGetAll(ctx, key).Result()
}

// Get history (last N readings)
func (r *RedisClient) getSensorHistory(sensorID int, count int) ([]string, error) {
	key := fmt.Sprintf("sensor:%d:history", sensorID)
	return r.client.LRange(ctx, key, 0, int64(count-1)).Result()
}

// Get all sensor IDs
func (r *RedisClient) getAllSensors() ([]string, error) {
	return r.client.SMembers(ctx, "sensors").Result()
}

// Store gate status
func (r *RedisClient) storeGateStatus(gateID int, isOpen bool, timestamp int64) error {
	key := fmt.Sprintf("gate:%d:latest", gateID)
	status := "closed"
	if isOpen {
		status = "open"
	}
	data := map[string]interface{}{
		"gate_id":   gateID,
		"status":    status,
		"is_open":   isOpen,
		"timestamp": timestamp,
	}

	r.client.SAdd(ctx, "gates", gateID)
	return r.client.HSet(ctx, key, data).Err()
}

// Get gate status
func (r *RedisClient) getGateStatus(gateID int) (map[string]string, error) {
	key := fmt.Sprintf("gate:%d:latest", gateID)
	return r.client.HGetAll(ctx, key).Result()
}

// Get all gate IDs
func (r *RedisClient) getAllGates() ([]string, error) {
	return r.client.SMembers(ctx, "gates").Result()
}

// ============================================================================
// MQTT HANDLER
// ============================================================================

type MQTTHandler struct {
	client mqtt.Client
	redis  *RedisClient
}

type SensorMessage struct {
	SensorID  int     `json:"sensor_id"`
	Type      string  `json:"type"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp int64   `json:"timestamp"`
}

type GateStatusMessage struct {
	GateID    int    `json:"gate_id"`
	Status    string `json:"status"`
	IsOpen    bool   `json:"is_open"`
	Timestamp int64  `json:"timestamp"`
}

func newMQTTHandler(brokerURL string, redisClient *RedisClient) *MQTTHandler {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID("cloud-server-" + strconv.FormatInt(time.Now().Unix(), 10))
	opts.SetAutoReconnect(true)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	handler := &MQTTHandler{redis: redisClient}
	opts.SetDefaultPublishHandler(handler.messageHandler)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("❌ Failed to connect to MQTT: %v", token.Error())
	}

	handler.client = client
	log.Println("✅ Connected to MQTT Broker")

	return handler
}

func (h *MQTTHandler) subscribe(topic string) {
	token := h.client.Subscribe(topic, 1, nil)
	token.Wait()
	log.Printf("📡 Subscribed to: %s", topic)
}

func (h *MQTTHandler) messageHandler(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()

	// Handle sensor data from farm/sensors/*/sensorID
	if strings.HasPrefix(topic, "farm/sensors/") {
		var sensorMsg SensorMessage
		if err := json.Unmarshal(msg.Payload(), &sensorMsg); err != nil {
			log.Printf("❌ Failed to parse sensor message: %v", err)
			return
		}

		// Store in Redis with full metadata
		err := h.redis.storeSensorReading(
			sensorMsg.SensorID,
			sensorMsg.Type,
			sensorMsg.Value,
			sensorMsg.Unit,
			sensorMsg.Lat,
			sensorMsg.Lon,
			sensorMsg.Timestamp,
		)
		if err != nil {
			log.Printf("❌ Failed to store sensor reading: %v", err)
			return
		}

		// Store in history
		h.redis.storeSensorHistory(sensorMsg.SensorID, sensorMsg.Value, sensorMsg.Timestamp)

		log.Printf("✅ Stored: Sensor %d (%s) = %.2f %s",
			sensorMsg.SensorID, sensorMsg.Type, sensorMsg.Value, sensorMsg.Unit)
	}

	// Handle gate status
	if strings.HasPrefix(topic, "farm/gates/") || strings.HasPrefix(topic, "gates/") {
		var gateMsg GateStatusMessage
		if err := json.Unmarshal(msg.Payload(), &gateMsg); err != nil {
			log.Printf("❌ Failed to parse gate message: %v", err)
			return
		}

		h.redis.storeGateStatus(gateMsg.GateID, gateMsg.IsOpen, gateMsg.Timestamp)
		log.Printf("✅ Stored: Gate %d = %s", gateMsg.GateID, gateMsg.Status)
	}
}

// ============================================================================
// HTTP HANDLERS
// ============================================================================

type APIHandlers struct {
	redis *RedisClient
}

func newAPIHandlers(redisClient *RedisClient) *APIHandlers {
	return &APIHandlers{redis: redisClient}
}

// GET /api/sensors (list all sensors with their latest data)
func (h *APIHandlers) listSensors(c *fiber.Ctx) error {
	sensorIDs, err := h.redis.getAllSensors()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	sensors := []map[string]string{}
	for _, idStr := range sensorIDs {
		id, _ := strconv.Atoi(idStr)
		data, err := h.redis.getLatestReading(id)
		if err == nil && len(data) > 0 {
			sensors = append(sensors, data)
		}
	}

	return c.JSON(fiber.Map{
		"sensors": sensors,
		"count":   len(sensors),
	})
}

// GET /api/sensors/:id/latest
func (h *APIHandlers) getLatestReading(c *fiber.Ctx) error {
	sensorID, _ := strconv.Atoi(c.Params("id"))
	data, err := h.redis.getLatestReading(sensorID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if len(data) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "Sensor not found"})
	}
	return c.JSON(data)
}

// GET /api/sensors/:id/history?limit=100
func (h *APIHandlers) getSensorHistory(c *fiber.Ctx) error {
	sensorID, _ := strconv.Atoi(c.Params("id"))
	limit, _ := strconv.Atoi(c.Query("limit", "100"))

	history, err := h.redis.getSensorHistory(sensorID, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"history": history, "count": len(history)})
}

// GET /api/gates (list all gates with their status)
func (h *APIHandlers) listGates(c *fiber.Ctx) error {
	gateIDs, err := h.redis.getAllGates()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	gates := []map[string]string{}
	for _, idStr := range gateIDs {
		id, _ := strconv.Atoi(idStr)
		status, err := h.redis.getGateStatus(id)
		if err == nil && len(status) > 0 {
			gates = append(gates, status)
		}
	}

	return c.JSON(fiber.Map{
		"gates": gates,
		"count": len(gates),
	})
}

// GET /api/gates/:id/status
func (h *APIHandlers) getGateStatus(c *fiber.Ctx) error {
	gateID, _ := strconv.Atoi(c.Params("id"))
	status, err := h.redis.getGateStatus(gateID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if len(status) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "Gate not found"})
	}
	return c.JSON(status)
}

// GET /api/stats (system statistics)
func (h *APIHandlers) getStats(c *fiber.Ctx) error {
	sensorIDs, _ := h.redis.getAllSensors()
	gateIDs, _ := h.redis.getAllGates()

	stats := fiber.Map{
		"total_sensors": len(sensorIDs),
		"total_gates":   len(gateIDs),
		"status":        "online",
		"timestamp":     time.Now().Unix(),
	}
	return c.JSON(stats)
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	log.Println("🚀 Starting Smart Farm Cloud Server...")

	config := loadConfig()
	redisClient := newRedisClient(config.RedisAddr)
	mqttHandler := newMQTTHandler(config.MQTTBroker, redisClient)

	// Subscribe to correct topics from simulator
	mqttHandler.subscribe("farm/sensors/#") // All sensor data
	mqttHandler.subscribe("farm/gates/#")   // All gate status
	mqttHandler.subscribe("gates/+/status") // Edge processor gate updates

	app := fiber.New(fiber.Config{
		AppName: "Smart Farm Cloud Server v1.0",
	})

	app.Use(logger.New())
	app.Use(cors.New())

	handlers := newAPIHandlers(redisClient)
	api := app.Group("/api")

	api.Get("/sensors", handlers.listSensors)
	api.Get("/sensors/:id/latest", handlers.getLatestReading)
	api.Get("/sensors/:id/history", handlers.getSensorHistory)

	api.Get("/gates", handlers.listGates)
	api.Get("/gates/:id/status", handlers.getGateStatus)

	api.Get("/stats", handlers.getStats)

	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service": "Smart Farm Cloud Server",
			"version": "1.0",
			"status":  "running",
			"endpoints": []string{
				"/api/sensors",
				"/api/sensors/:id/latest",
				"/api/sensors/:id/history",
				"/api/gates",
				"/api/gates/:id/status",
				"/api/stats",
			},
		})
	})

	log.Printf("🌐 Server listening on http://localhost%s", config.HTTPPort)
	log.Fatal(app.Listen(config.HTTPPort))
}
