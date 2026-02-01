## System Components
The project consists of four main programs:

1. **Sensor Simulator**
   - Simulates soil moisture, temperature, water flow, weather, and water level sensors
   - Publishes sensor data via MQTT
   - Reacts to water gate open/close commands

2. **Edge Processor**
   - Receives soil moisture data
   - Makes irrigation decisions
   - Opens or closes water gates automatically

3. **Cloud Server**
   - Receives all sensor and gate data
   - Stores data in Redis
   - Provides REST APIs for monitoring

4. **Water Gate Test Tool**
   - Manual tool to test gate open/close commands

---

## Technologies Used
- **Language:** Go (Golang)
- **Messaging Protocol:** MQTT (Mosquitto)
- **Database:** Redis
- **Web Framework:** Fiber (Cloud Server)
- **Data Format:** JSON
- **Architecture:** IoT + Edge + Cloud

---

## Project Structure

CropMind/

├── cloud/

│ └── cloud-server/

│ └── main.go

├── edge/

│ └── main.go

├── simulator/

│ ├── main.go

│ └── main.json

├── water-gate-test/

│ └── main.go

└── README.md


---

## Prerequisites
Make sure the following services are installed and running:

- **Go** (version 1.20 or newer)
- **Mosquitto MQTT Broker**
- **Redis Server**

### Start required services
```bash
# Start MQTT broker
mosquitto

# Start Redis server
redis-server

✅ Order of Running the Programs

⚠️ Important: The system must be started in the following order.
1️⃣ Run the Cloud Server

Stores data and provides REST APIs.

                                                                    bash
cd cloud/cloud-server
go run main.go

Runs on:

                                                                    text
http://localhost:8080

2️⃣ Run the Edge Processor

Controls irrigation logic and water gates.

                                                                    bash
cd edge
go run main.go

The edge processor:

    Subscribes to sensor topics
    Opens gates if soil moisture < 40%
    Closes gates if soil moisture > 70%

3️⃣ Run the Sensor Simulator

Generates sensor data and listens for gate commands.

                                                                    bash
cd simulator
go run main.go

    Select a scenario (1–5)
    Set publishing interval (default: 5 seconds)

4️⃣ (Optional) Run Water Gate Test Tool

Used only for manual gate testing.

                                                                    bash
cd water-gate-test
go run main.go

Cloud Server API Endpoints
Endpoint 	Description
/api/sensors 	List all sensors with latest data
/api/sensors/:id/latest 	Latest sensor reading
/api/sensors/:id/history 	Sensor history
/api/gates 	List all water gates
/api/gates/:id/status 	Gate status
/api/stats 	System statistics
Irrigation Logic (Edge Computing)

    Soil moisture below 40% → Water gate opens
    Soil moisture above 70% → Water gate closes
    A cooldown time prevents frequent gate switching

This simulates a real smart irrigation decision process.
Notes

    This project does not use real hardware
    Designed for educational and university use
    All sensor data is simulated
    MQTT topics follow the format:
        farm/sensors/<sensor-type>/<sensor-id>
        farm/commands/water-gate-sensors/<gate-id>