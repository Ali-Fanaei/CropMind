package main

import (
	"encoding/json"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type GateCommand struct {
	GateID    int    `json:"gate_id"`
	Action    string `json:"action"`
	Timestamp int64  `json:"timestamp"`
}

func main() {
	// Connect to MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://localhost:1883")
	opts.SetClientID("gate-tester")

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Printf("❌ Connection failed: %v\n", token.Error())
		return
	}
	defer client.Disconnect(250)

	fmt.Println("🚰 Testing Gate Commands...\n")

	// Test sequence
	tests := []struct {
		gateID int
		action string
		wait   int
	}{
		{1, "OPEN", 3},
		{2, "OPEN", 3},
		{1, "CLOSE", 3},
		{2, "CLOSE", 0},
	}

	for _, test := range tests {
		sendCommand(client, test.gateID, test.action)
		if test.wait > 0 {
			time.Sleep(time.Duration(test.wait) * time.Second)
		}
	}

	fmt.Println("\n✓ Test complete!")
}

func sendCommand(client mqtt.Client, gateID int, action string) {
	cmd := GateCommand{
		GateID:    gateID,
		Action:    action,
		Timestamp: time.Now().Unix(),
	}

	payload, _ := json.Marshal(cmd)
	topic := fmt.Sprintf("farm/commands/water-gate-sensors/%d", gateID)

	client.Publish(topic, 0, false, payload)
	fmt.Printf("📤 Gate #%d → %s\n", gateID, action)
}
