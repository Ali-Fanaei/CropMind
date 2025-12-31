package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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
		RedisAddr:  "localhost:6379", // Docker Redis
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

	// Test connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}

	log.Println("✅ Connected to Redis")
	return &RedisClient{client: rdb}
}

// Store latest sensor reading
func (r *RedisClient) storeSensorReading(sensorID int, value float64, timestamp int64) error {
	key := fmt.Sprintf("sensor:%d:latest", sensorID)
	data := map[string]interface{}{
		"value":     value,
		"timestamp": timestamp,
	}
	return r.client.HSet(ctx, key, data).Err()
}

// Store reading in history (keep last 1000)
func (r *RedisClient) storeSensorHistory(sensorID int, value float64, timestamp int64) error {
	key := fmt.Sprintf("sensor:%d:history", sensorID)
	data := fmt.Sprintf(`{"value":%.2f,"timestamp":%d}`, value, timestamp)

	pipe := r.client.Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, 999) // Keep only last 1000
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

// Store gate status
func (r *RedisClient) storeGateStatus(gateID int, status string, timestamp int64) error {
	key := fmt.Sprintf("gate:%d:latest", gateID)
	data := map[string]interface{}{
		"status":    status,
		"timestamp": timestamp,
	}
	return r.client.HSet(ctx, key, data).Err()
}

// Get gate status
func (r *RedisClient) getGateStatus(gateID int) (map[string]string, error) {
	key := fmt.Sprintf("gate:%d:latest", gateID)
	return r.client.HGetAll(ctx, key).Result()
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
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
}

type GateMessage struct {
	GateID    int    `json:"gate_id"`
	Status    string `json:"status"`
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
	log.Printf("📨 [%s] %s", msg.Topic(), string(msg.Payload()))

	// Handle sensor data
	if len(msg.Topic()) > 8 && msg.Topic()[:8] == "sensors/" {
		var sensorMsg SensorMessage
		if err := json.Unmarshal(msg.Payload(), &sensorMsg); err != nil {
			log.Printf("❌ Failed to parse sensor message: %v", err)
			return
		}

		// Store in Redis
		h.redis.storeSensorReading(sensorMsg.SensorID, sensorMsg.Value, sensorMsg.Timestamp)
		h.redis.storeSensorHistory(sensorMsg.SensorID, sensorMsg.Value, sensorMsg.Timestamp)

		log.Printf("✅ Stored: Sensor %d = %.2f", sensorMsg.SensorID, sensorMsg.Value)
	}

	// Handle gate status
	if len(msg.Topic()) > 6 && msg.Topic()[:6] == "gates/" {
		var gateMsg GateMessage
		if err := json.Unmarshal(msg.Payload(), &gateMsg); err != nil {
			log.Printf("❌ Failed to parse gate message: %v", err)
			return
		}

		h.redis.storeGateStatus(gateMsg.GateID, gateMsg.Status, gateMsg.Timestamp)
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

// GET /api/sensors (list all sensors)
func (h *APIHandlers) listSensors(c *fiber.Ctx) error {
	sensors := []map[string]interface{}{
		{"id": 101, "type": "soil-moisture", "location": []float64{35.7, 51.4}},
		{"id": 102, "type": "soil-moisture", "location": []float64{35.71, 51.41}},
		{"id": 103, "type": "soil-moisture", "location": []float64{35.72, 51.42}},
	}
	return c.JSON(sensors)
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

// GET /api/gates (list all gates)
func (h *APIHandlers) listGates(c *fiber.Ctx) error {
	gates := []map[string]interface{}{
		{"id": 201, "type": "irrigation", "location": []float64{35.7, 51.4}},
		{"id": 202, "type": "irrigation", "location": []float64{35.71, 51.41}},
	}
	return c.JSON(gates)
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
	stats := fiber.Map{
		"total_sensors": 3,
		"total_gates":   2,
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

	// Load configuration
	config := loadConfig()

	// Connect to Redis
	redisClient := newRedisClient(config.RedisAddr)

	// Connect to MQTT
	mqttHandler := newMQTTHandler(config.MQTTBroker, redisClient)
	mqttHandler.subscribe("sensors/+/data")
	mqttHandler.subscribe("gates/+/status")

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName: "Smart Farm Cloud Server v1.0",
	})

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New())

	// API Routes
	handlers := newAPIHandlers(redisClient)
	api := app.Group("/api")

	// Sensor endpoints
	api.Get("/sensors", handlers.listSensors)
	api.Get("/sensors/:id/latest", handlers.getLatestReading)
	api.Get("/sensors/:id/history", handlers.getSensorHistory)

	// Gate endpoints
	api.Get("/gates", handlers.listGates)
	api.Get("/gates/:id/status", handlers.getGateStatus)

	// Stats endpoint
	api.Get("/stats", handlers.getStats)

	// Root endpoint
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

	// Start server
	log.Printf("🌐 Server listening on http://localhost%s", config.HTTPPort)
	log.Fatal(app.Listen(config.HTTPPort))
}
