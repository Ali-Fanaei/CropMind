package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ============================================================================
// DATA STRUCTURES FOR GEOJSON
// ============================================================================

// GeoJSON represents the root structure of each sensor layer
type GeoJSON struct {
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	Features []Feature `json:"features"`
}

// Feature represents individual sensor in the GeoJSON
type Feature struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Geometry   Geometry               `json:"geometry"`
}

// Geometry holds the coordinate data
type Geometry struct {
	Type        string      `json:"type"`
	Coordinates interface{} `json:"coordinates"`
}

// SensorData is what we publish to MQTT
type SensorData struct {
	SensorID  int     `json:"sensor_id"`
	Type      string  `json:"type"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp int64   `json:"timestamp"`
}

// GateCommand represents commands sent to water gates
type GateCommand struct {
	GateID    int    `json:"gate_id"`
	Action    string `json:"action"` // "OPEN" or "CLOSE"
	Timestamp int64  `json:"timestamp"`
}

// ============================================================================
// SCENARIO DEFINITIONS
// ============================================================================

// Range defines min/max values for sensor readings
type Range struct {
	Min float64
	Max float64
}

// WaterFlowRanges defines flow rates based on gate status
type WaterFlowRanges struct {
	GatesOpen   Range // Flow when ANY gate is open
	GatesClosed Range // Flow when ALL gates are closed
}

// SensorRanges holds ranges for all sensor types in a scenario
type SensorRanges struct {
	SoilMoisture    Range
	SoilTemperature Range
	WaterFlow       WaterFlowRanges // ← NOW GATE-DEPENDENT!
	WaterLevel      Range
	WeatherTemp     Range
}

// Scenario represents a predefined farm condition
type Scenario struct {
	Name        string
	Description string
	Ranges      SensorRanges
}

// All available scenarios
var Scenarios = map[int]Scenario{
	1: {
		Name:        "Normal Day",
		Description: "Perfect conditions, everything optimal",
		Ranges: SensorRanges{
			SoilMoisture:    Range{Min: 45, Max: 65},
			SoilTemperature: Range{Min: 18, Max: 24},
			WaterFlow: WaterFlowRanges{
				GatesOpen:   Range{Min: 15, Max: 25}, // Active flow
				GatesClosed: Range{Min: 0, Max: 2},   // Minimal residual
			},
			WaterLevel:  Range{Min: 70, Max: 90},
			WeatherTemp: Range{Min: 20, Max: 28},
		},
	},
	2: {
		Name:        "Drought Alert",
		Description: "Low moisture, high temperature, need irrigation",
		Ranges: SensorRanges{
			SoilMoisture:    Range{Min: 15, Max: 30},
			SoilTemperature: Range{Min: 28, Max: 38},
			WaterFlow: WaterFlowRanges{
				GatesOpen:   Range{Min: 20, Max: 35}, // Maximum irrigation
				GatesClosed: Range{Min: 0, Max: 1},   // Nearly zero
			},
			WaterLevel:  Range{Min: 40, Max: 60},
			WeatherTemp: Range{Min: 32, Max: 42},
		},
	},
	3: {
		Name:        "Heavy Rain",
		Description: "High moisture, gates should close",
		Ranges: SensorRanges{
			SoilMoisture:    Range{Min: 75, Max: 95},
			SoilTemperature: Range{Min: 12, Max: 18},
			WaterFlow: WaterFlowRanges{
				GatesOpen:   Range{Min: 30, Max: 50}, // Rain + irrigation
				GatesClosed: Range{Min: 0, Max: 3},   // Rain runoff only
			},
			WaterLevel:  Range{Min: 85, Max: 100},
			WeatherTemp: Range{Min: 12, Max: 20},
		},
	},
	4: {
		Name:        "Active Irrigation",
		Description: "System irrigating, water gates open",
		Ranges: SensorRanges{
			SoilMoisture:    Range{Min: 35, Max: 55},
			SoilTemperature: Range{Min: 20, Max: 26},
			WaterFlow: WaterFlowRanges{
				GatesOpen:   Range{Min: 25, Max: 40}, // Full irrigation
				GatesClosed: Range{Min: 0, Max: 2},
			},
			WaterLevel:  Range{Min: 50, Max: 80},
			WeatherTemp: Range{Min: 22, Max: 30},
		},
	},
	5: {
		Name:        "Frost Warning",
		Description: "Low temperature, trees at risk",
		Ranges: SensorRanges{
			SoilMoisture:    Range{Min: 40, Max: 60},
			SoilTemperature: Range{Min: -2, Max: 5},
			WaterFlow: WaterFlowRanges{
				GatesOpen:   Range{Min: 10, Max: 20}, // Slow flow in cold
				GatesClosed: Range{Min: 0, Max: 1},
			},
			WaterLevel:  Range{Min: 60, Max: 85},
			WeatherTemp: Range{Min: -5, Max: 3},
		},
	},
}

// ============================================================================
// SIMULATOR
// ============================================================================

// Simulator manages MQTT connection and sensor data generation
type Simulator struct {
	client        mqtt.Client
	sensors       []GeoJSON
	scenario      Scenario
	anyGateOpen   bool       // ← NEW: Tracks if ANY gate is open
	gateStatusMux sync.Mutex // ← NEW: Thread-safe gate status updates
}

// NewSimulator creates and connects to MQTT broker
func NewSimulator(broker string, sensors []GeoJSON) (*Simulator, error) {
	sim := &Simulator{
		sensors:     sensors,
		anyGateOpen: false, // All gates start closed
	}

	// Configure MQTT client
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("sensor-simulator")
	opts.SetDefaultPublishHandler(sim.handleMessage) // ← NEW: Listen to all messages

	// Connect to broker
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	sim.client = client
	fmt.Println("✓ Connected to MQTT broker")

	// Subscribe to ALL gate command topics
	// Pattern: farm/commands/water-gate-sensors/+
	topic := "farm/commands/water-gate-sensors/+"
	if token := client.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	fmt.Printf("✓ Subscribed to gate commands: %s\n", topic)

	return sim, nil
}

// handleMessage processes incoming MQTT messages (gate commands)
func (s *Simulator) handleMessage(client mqtt.Client, msg mqtt.Message) {
	// Parse gate command
	var cmd GateCommand
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		return // Invalid message, ignore
	}

	// Update gate status
	s.gateStatusMux.Lock()
	defer s.gateStatusMux.Unlock()

	if cmd.Action == "OPEN" {
		s.anyGateOpen = true
		fmt.Printf("🚰 Gate #%d OPENED → Water flowing!\n", cmd.GateID)
	} else if cmd.Action == "CLOSE" {
		// In simple mode, we assume if we get a CLOSE, all gates might be closed
		// (In real system, you'd track each gate individually)
		s.anyGateOpen = false
		fmt.Printf("🚫 Gate #%d CLOSED → Water stopped\n", cmd.GateID)
	}
}

// SetScenario configures which scenario to simulate
func (s *Simulator) SetScenario(scenario Scenario) {
	s.scenario = scenario
	fmt.Printf("✓ Scenario set: %s\n", scenario.Name)
}

// Start begins the continuous simulation loop
func (s *Simulator) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		s.publishAll() // Publish all sensor data every interval
	}
}

// publishAll generates and publishes data for all sensors
func (s *Simulator) publishAll() {
	// Loop through each sensor type (soil moisture, temperature, etc.)
	for _, geoJSON := range s.sensors {
		sensorType := geoJSON.Name

		// ⚠️ SKIP WATER GATES - They are actuators, not sensors!
		if sensorType == "water-gate-sensors" {
			continue // Don't simulate gate status
		}

		// Loop through each individual sensor in this type
		for _, feature := range geoJSON.Features {
			// Extract sensor ID from properties
			id := int(feature.Properties["id"].(float64))

			// Extract coordinates
			lat, lon := ExtractCoordinates(feature.Geometry.Coordinates)

			// Generate value based on current scenario AND gate status
			value := s.generateValue(sensorType)

			// Create sensor data packet
			data := SensorData{
				SensorID:  id,
				Type:      sensorType,
				Lat:       lat,
				Lon:       lon,
				Value:     value,
				Unit:      s.getUnit(sensorType),
				Timestamp: time.Now().Unix(),
			}

			// Publish to MQTT
			s.publish(data)
		}
	}
}

// generateValue creates a random value within scenario range
func (s *Simulator) generateValue(sensorType string) float64 {
	var r Range

	// Select the appropriate range based on sensor type
	switch sensorType {
	case "soil-moisture-sensors":
		r = s.scenario.Ranges.SoilMoisture
	case "soil-temperature-sensors":
		r = s.scenario.Ranges.SoilTemperature
	case "water-flow-sensors":
		// ← NEW: Flow depends on gate status!
		s.gateStatusMux.Lock()
		if s.anyGateOpen {
			r = s.scenario.Ranges.WaterFlow.GatesOpen
		} else {
			r = s.scenario.Ranges.WaterFlow.GatesClosed
		}
		s.gateStatusMux.Unlock()
	case "water-level-sensor":
		r = s.scenario.Ranges.WaterLevel
	case "weather-sensor":
		r = s.scenario.Ranges.WeatherTemp
	default:
		return 0
	}

	// Generate random value between min and max
	return r.Min + rand.Float64()*(r.Max-r.Min)
}

// getUnit returns the measurement unit for each sensor type
func (s *Simulator) getUnit(sensorType string) string {
	units := map[string]string{
		"soil-moisture-sensors":    "%",
		"soil-temperature-sensors": "°C",
		"water-flow-sensors":       "L/min",
		"water-level-sensor":       "%",
		"weather-sensor":           "°C",
	}
	return units[sensorType]
}

// publish sends sensor data to MQTT topic
func (s *Simulator) publish(data SensorData) {
	// Create topic: farm/sensor-type/sensor-id
	topic := fmt.Sprintf("farm/%s/%d", data.Type, data.SensorID)

	// Convert data to JSON
	payload, _ := json.Marshal(data)

	// Publish to MQTT (QoS 0, not retained)
	s.client.Publish(topic, 0, false, payload)

	// Print to console (with gate status indicator for flow sensors)
	if data.Type == "water-flow-sensors" {
		s.gateStatusMux.Lock()
		gateStatus := "🚫"
		if s.anyGateOpen {
			gateStatus = "🚰"
		}
		s.gateStatusMux.Unlock()
		fmt.Printf("📡 %s [%d]: %.2f %s %s\n", data.Type, data.SensorID, data.Value, data.Unit, gateStatus)
	} else {
		fmt.Printf("📡 %s [%d]: %.2f %s\n", data.Type, data.SensorID, data.Value, data.Unit)
	}
}

// Close disconnects from MQTT broker
func (s *Simulator) Close() {
	s.client.Disconnect(250)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// LoadSensors reads and parses the main.json file
func LoadSensors(filepath string) ([]GeoJSON, error) {
	// Read file contents
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	// Parse JSON into GeoJSON structures
	var geoJSONs []GeoJSON
	err = json.Unmarshal(data, &geoJSONs)
	if err != nil {
		return nil, err
	}

	return geoJSONs, nil
}

// ExtractCoordinates handles different GeoJSON coordinate formats
func ExtractCoordinates(coords interface{}) (float64, float64) {
	switch v := coords.(type) {
	case []interface{}: // Point coordinates [lon, lat]
		if len(v) >= 2 {
			lon := v[0].(float64)
			lat := v[1].(float64)
			return lat, lon
		}
	case [][][]interface{}: // Polygon coordinates
		if len(v) > 0 && len(v[0]) > 0 && len(v[0][0]) >= 2 {
			lon := v[0][0][0].(float64)
			lat := v[0][0][1].(float64)
			return lat, lon
		}
	}
	return 0, 0
}

// ============================================================================
// MAIN FUNCTION
// ============================================================================

func main() {
	// Print header
	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║   Smart Farm Sensor Simulator         ║")
	fmt.Println("║   (Gate-Responsive Flow Sensors)      ║")
	fmt.Println("╚═══════════════════════════════════════╝\n")

	// Load sensors from JSON file
	geoJSONs, err := LoadSensors("main.json")
	if err != nil {
		fmt.Printf("❌ Error loading sensors: %v\n", err)
		return
	}

	// Count and display loaded sensors (excluding actuators)
	totalSensors := 0
	for _, gj := range geoJSONs {
		count := len(gj.Features)

		// Mark actuators differently
		if gj.Name == "water-gate-sensors" {
			fmt.Printf("⚙️  Found %d %s (actuators - controlled by Edge)\n", count, gj.Name)
		} else {
			fmt.Printf("✓ Loaded %d %s\n", count, gj.Name)
			totalSensors += count
		}
	}
	fmt.Printf("\n📊 Total active sensors: %d\n\n", totalSensors)

	// Create simulator and connect to MQTT broker
	broker := "tcp://localhost:1883"
	sim, err := NewSimulator(broker, geoJSONs)
	if err != nil {
		fmt.Printf("❌ MQTT connection failed: %v\n", err)
		fmt.Println("💡 Make sure mosquitto is running")
		fmt.Println("   Windows: mosquitto -v")
		return
	}
	defer sim.Close() // Disconnect when program exits

	// Display available scenarios
	fmt.Println("\n🎯 Available Scenarios:")
	fmt.Println("════════════════════════════════════════")
	for id := 1; id <= 5; id++ {
		scenario := Scenarios[id]
		fmt.Printf("[%d] %s\n    %s\n", id, scenario.Name, scenario.Description)
	}
	fmt.Println("════════════════════════════════════════")

	// Get user input for scenario selection
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nSelect scenario (1-5): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > 5 {
		fmt.Println("❌ Invalid choice")
		return
	}

	// Set the selected scenario
	scenario := Scenarios[choice]
	sim.SetScenario(scenario)

	// Get publishing interval from user
	fmt.Print("Publishing interval (seconds, default 5): ")
	intervalInput, _ := reader.ReadString('\n')
	intervalInput = strings.TrimSpace(intervalInput)
	interval := 5
	if val, err := strconv.Atoi(intervalInput); err == nil && val > 0 {
		interval = val
	}

	// Start simulation
	fmt.Printf("\n🚀 Starting simulation...\n")
	fmt.Printf("📤 Publishing every %d seconds\n", interval)
	fmt.Println("🎧 Listening for gate commands on: farm/commands/water-gate-sensors/+")
	fmt.Println("⚙️  Water flow sensors will react to gate status\n")
	fmt.Println("Press Ctrl+C to stop\n")

	// Begin continuous publishing loop
	sim.Start(time.Duration(interval) * time.Second)
}
