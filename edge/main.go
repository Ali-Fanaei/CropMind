package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// SensorData represents incoming sensor data
type SensorData struct {
	SensorID  int     `json:"sensor_id"`
	Type      string  `json:"type"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp int64   `json:"timestamp"`
}

// GateState tracks the current state of each water gate
type GateState struct {
	GateID      int
	IsOpen      bool
	LastCommand time.Time
}

// Global state
var (
	gateStates         = make(map[int]*GateState)
	soilMoistureStates = make(map[int]float64)
	stateMutex         sync.RWMutex
)

// Configuration
const (
	mqttBroker      = "tcp://localhost:1883"
	dryThreshold    = 40.0 // Below this → open gate
	wetThreshold    = 70.0 // Above this → close gate
	commandCooldown = 30 * time.Second
)

// Sensor ID to Gate ID mapping
var sensorToGateMap = map[int]int{
	// Gate 1 controls sensors 9001-9019
	9001: 1, 9002: 1, 9003: 1, 9004: 1, 9005: 1,
	9006: 1, 9007: 1, 9008: 1, 9009: 1, 9010: 1,
	9011: 1, 9012: 1, 9013: 1, 9014: 1, 9015: 1,
	9016: 1, 9017: 1, 9018: 1, 9019: 1,

	// Gate 2 controls sensors 9020-9038
	9020: 2, 9021: 2, 9022: 2, 9023: 2, 9024: 2,
	9025: 2, 9026: 2, 9027: 2, 9028: 2, 9029: 2,
	9030: 2, 9031: 2, 9032: 2, 9033: 2, 9034: 2,
	9035: 2, 9036: 2, 9037: 2, 9038: 2,
}

// MQTT client
var client mqtt.Client

// ============================================
// INITIALIZATION
// ============================================

func initializeGateStates() {
	for gateID := 1; gateID <= 22; gateID++ {
		gateStates[gateID] = &GateState{
			GateID:      gateID,
			IsOpen:      false,
			LastCommand: time.Time{}, // Zero time (very old)
		}
	}
	fmt.Println("✅ Initialized 22 gates (all CLOSED)")
}

// ============================================
// MQTT HANDLERS
// ============================================

var messageHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	var data SensorData
	if err := json.Unmarshal(msg.Payload(), &data); err != nil {
		log.Printf("❌ Error parsing message: %v", err)
		return
	}

	// Format timestamp
	timestamp := time.Unix(data.Timestamp, 0).Format("15:04:05")

	// Log received message
	fmt.Printf("📥 Received: Topic=%s | Payload=%s\n", msg.Topic(), string(msg.Payload()))

	// Handle different sensor types
	switch data.Type {
	case "soil-moisture-sensors":
		fmt.Printf("%s 📊 Soil Moisture [%d]: %.2f%% at %s\n",
			timestamp, data.SensorID, data.Value, timestamp)
		handleSoilMoisture(data)

	case "water-flow-sensors":
		icon := "🚫"
		if data.Value > 10.0 {
			icon = "🚰"
		}
		fmt.Printf("%s 💧 Water Flow [%d]: %.2f %s %s\n",
			timestamp, data.SensorID, data.Value, data.Unit, icon)

	case "soil-temperature-sensors":
		fmt.Printf("%s 🌡️ Soil Temp [%d]: %.2f%s\n",
			timestamp, data.SensorID, data.Value, data.Unit)
	}
}

func handleSoilMoisture(data SensorData) {
	stateMutex.Lock()
	soilMoistureStates[data.SensorID] = data.Value
	stateMutex.Unlock()

	// Evaluate if irrigation action is needed
	evaluateIrrigationNeeds(data.SensorID, data.Value)
}

// ============================================
// DECISION LOGIC
// ============================================

func evaluateIrrigationNeeds(sensorID int, moistureLevel float64) {
	fmt.Printf("🔍 DEBUG: Evaluating sensor %d with moisture %.2f%%\n", sensorID, moistureLevel)

	// Find which gate controls this sensor
	gateID, exists := sensorToGateMap[sensorID]
	if !exists {
		fmt.Printf("⚠️ DEBUG: Sensor %d not mapped to any gate\n", sensorID)
		return // Unknown sensor
	}
	fmt.Printf("✅ DEBUG: Sensor %d mapped to Gate %d\n", sensorID, gateID)

	stateMutex.Lock()
	defer stateMutex.Unlock()

	gate := gateStates[gateID]
	fmt.Printf("🚪 DEBUG: Gate %d current state: IsOpen=%v, LastCommand=%v\n",
		gateID, gate.IsOpen, gate.LastCommand)

	// Check cooldown
	timeSinceLastCommand := time.Since(gate.LastCommand)
	fmt.Printf("⏱️ DEBUG: Time since last command: %v (cooldown: %v)\n",
		timeSinceLastCommand, commandCooldown)

	if timeSinceLastCommand < commandCooldown {
		fmt.Printf("❌ DEBUG: Still in cooldown period, skipping\n")
		return
	}

	// Decision logic
	fmt.Printf("📊 DEBUG: Checking thresholds - Moisture: %.2f%% | Dry: %.2f%% | Wet: %.2f%%\n",
		moistureLevel, dryThreshold, wetThreshold)

	if moistureLevel < dryThreshold && !gate.IsOpen {
		// Too dry - open gate
		fmt.Printf("✅ DEBUG: Condition met! Moisture %.2f%% < %.2f%% AND gate is closed\n",
			moistureLevel, dryThreshold)
		sendGateCommand(gateID, "OPEN", fmt.Sprintf("Soil moisture %.2f%% below threshold %.2f%%", moistureLevel, dryThreshold))
		gate.IsOpen = true
		gate.LastCommand = time.Now()
	} else if moistureLevel > wetThreshold && gate.IsOpen {
		// Too wet - close gate
		fmt.Printf("✅ DEBUG: Condition met! Moisture %.2f%% > %.2f%% AND gate is open\n",
			moistureLevel, wetThreshold)
		sendGateCommand(gateID, "CLOSE", fmt.Sprintf("Soil moisture %.2f%% above threshold %.2f%%", moistureLevel, wetThreshold))
		gate.IsOpen = false
		gate.LastCommand = time.Now()
	} else {
		fmt.Printf("❌ DEBUG: No action needed - Moisture: %.2f%%, Gate Open: %v\n",
			moistureLevel, gate.IsOpen)
	}
	fmt.Println()
}

// ============================================
// COMMAND EXECUTION
// ============================================

func sendGateCommand(gateID int, command string, reason string) {
	topic := fmt.Sprintf("farm/commands/water-gate-sensors/%d", gateID)
	payload := map[string]interface{}{
		"gate_id":   gateID,
		"command":   command,
		"reason":    reason,
		"timestamp": time.Now().Unix(),
	}

	payloadBytes, _ := json.Marshal(payload)
	token := client.Publish(topic, 0, false, payloadBytes)
	token.Wait()

	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("%s 🚰 COMMAND: Gate #%d → %s | Reason: %s\n",
		timestamp, gateID, command, reason)
}

// ============================================
// MQTT CONNECTION
// ============================================

func connectMQTT() mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttBroker)
	opts.SetClientID("edge-processor")
	opts.SetDefaultPublishHandler(messageHandler)
	opts.SetAutoReconnect(true)
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("⚠️ Connection lost: %v", err)
	})
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("✅ Connected to MQTT broker")
	})

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("❌ Failed to connect to MQTT broker: %v", token.Error())
	}

	return client
}

// ============================================
// MAIN
// ============================================

func main() {
	fmt.Println("🌾 EDGE PROCESSOR - Smart Farm 🌾")
	fmt.Println("Automated Irrigation Controller")
	fmt.Println("======================================\n")

	// Initialize state
	initializeGateStates()

	// Connect to MQTT
	client = connectMQTT()
	defer client.Disconnect(250)

	// Display configuration
	fmt.Printf("🔧 Configuration:\n")
	fmt.Printf("   • Dry threshold: %.2f%%\n", dryThreshold)
	fmt.Printf("   • Wet threshold: %.2f%%\n", wetThreshold)
	fmt.Printf("   • Min command interval: %v\n", commandCooldown)
	fmt.Printf("   • Sensor-to-Gate mapping: %d sensors configured\n\n", len(sensorToGateMap))

	// Subscribe to sensor topics
	topics := []string{
		"farm/sensors/soil-moisture-sensors/+",
		"farm/sensors/water-flow-sensors/+",
		"farm/sensors/soil-temperature-sensors/+",
	}

	for _, topic := range topics {
		if token := client.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
			log.Fatalf("❌ Failed to subscribe to %s: %v", topic, token.Error())
		}
		fmt.Printf("✅ Subscribed to: %s\n", topic)
	}

	fmt.Println("\n🚀 Edge Processor is running... (Press Ctrl+C to stop)")
	fmt.Println("\n⏳ Waiting for sensor data...\n")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n👋 Shutting down gracefully...")
}
