<details>

<summary>Click to expand README.md content (copy entire content below)</summary>

        

markdown
# 🌾 IoT Farm Monitoring System

A complete IoT solution for real-time farm monitoring using MQTT, Redis, and Go. This system simulates sensor data, processes it through a cloud server, and displays it on a real-time dashboard.

## 📋 Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Running the System](#running-the-system)
- [Usage](#usage)
- [API Documentation](#api-documentation)
- [Troubleshooting](#troubleshooting)
- [Configuration](#configuration)

---

## ✨ Features

- **Real-time Sensor Monitoring**: Soil moisture, temperature, humidity, and pH sensors
- **Automated Gate Control**: Monitor farm gate status
- **Live Dashboard**: Web-based interface with real-time updates every 5 seconds
- **Historical Data**: Track sensor readings over time (last 100 readings)
- **RESTful API**: Access data programmatically
- **MQTT Protocol**: Efficient pub/sub messaging
- **Redis Storage**: Fast in-memory data access and retrieval

---

## 🏗️ Architecture

┌─────────────┐ MQTT ┌──────────────┐ Redis ┌────────────┐

│ Simulator │ ────────────> │ Mosquitto │ ────────────> │ Redis │

│ (Go) │ Publish │ Broker │ Subscribe │ Database │

└─────────────┘ └──────────────┘ └────────────┘

│ │

│ │

▼ ▼

┌──────────────┐ ┌────────────┐

│ Cloud Server │◄─────────────│ REST API │

│ (Go) │ Query │ (Fiber) │

└──────────────┘ └────────────┘

│

▼

┌──────────────┐

│ Dashboard │

│ (HTML/JS) │

└──────────────┘

Data Flow:

    Simulator generates random sensor data every 2 seconds
    Data published to MQTT broker on topics like farm/sensors/soil-moisture-sensors/9001
    Cloud server subscribes to farm/sensors/# and receives all sensor messages
    Cloud server stores data in Redis (latest reading + history)
    Dashboard fetches data via REST API and updates UI in real-time

📦 Prerequisites

Before installation, ensure you have the following installed:
Required Software
Software 	Version 	Purpose
Docker 	20.10+ 	Container runtime
Docker Compose 	1.29+ 	Multi-container orchestration
Go 	1.21+ 	Application runtime
Git 	2.0+ 	Version control
Installation Links

    Docker Installation Guide
    Go Installation Guide
    Git Installation Guide

System Requirements

    OS: Linux, macOS, or Windows with WSL2
    RAM: Minimum 2GB available
    Disk: 500MB free space
    Network: Ports 1883 (MQTT), 6379 (Redis), 3001 (API) must be available

Verify Installation

bash
Check Docker

docker --version
Expected: Docker version 20.10.x or higher
Check Docker Compose

docker-compose --version
Expected: docker-compose version 1.29.x or higher
Check Go

go version
Expected: go version go1.21.x or higher
Check Git

git --version
Expected: git version 2.x.x or higher
🚀 Installation
Step 1: Clone the Repository

bash

git clone https://github.com/yourusername/iot-farm-monitoring.git

cd iot-farm-monitoring
Step 2: Install Go Dependencies

bash
For Cloud Server

cd cloud-server

go mod download

cd …
For Simulator

cd simulator

go mod download

cd …
Step 3: Start Infrastructure Services

bash
Start Redis and Mosquitto MQTT Broker

docker-compose up -d
Verify services are running

docker-compose ps
Step 4: Verify Infrastructure

bash
Test Redis connection

docker exec -it redis redis-cli ping
Expected output: PONG
Test Mosquitto MQTT broker

docker exec -it mosquitto mosquitto_sub -t test -C 1 &

docker exec -it mosquitto mosquitto_pub -t test -m “hello”
Expected output: hello
▶️ Running the System

IMPORTANT: Follow this exact order to start all components.
1️⃣ Start Cloud Server (First)

bash

cd cloud-server

go run main.go

Expected Output:

2026/01/01 10:00:00 Connected to Redis successfully

2026/01/01 10:00:00 Connected to MQTT Broker successfully

2026/01/01 10:00:00 Subscribed to topics: farm/sensors/#, farm/gates/#

2026/01/01 10:00:00 Server started on :3001
2️⃣ Start Simulator (Second)

Open a new terminal window:

bash

cd simulator

go run main.go

Expected Output:

2026/01/01 10:00:05 Farm Simulator Started

2026/01/01 10:00:05 Connected to MQTT Broker

2026/01/01 10:00:05 Publishing to: farm/sensors/soil-moisture-sensors/9001
3️⃣ Access Dashboard (Third)

Open your web browser:

http://localhost:3001
🖥️ Usage
Dashboard Interface
1. Overview Tab

    Total Sensors count
    Latest readings from all sensors
    Auto-refresh every 5 seconds

2. Sensors Tab

    Detailed sensor cards
    Current values with units
    GPS coordinates
    “View History” button - Shows Chart.js graph

3. Gates Tab

    Gate status (Open/Closed)
    Color-coded indicators

Testing the API with curl

bash
Health check

curl http://localhost:3001/health
Get all sensors

curl http://localhost:3001/api/sensors
Get specific sensor

curl http://localhost:3001/api/sensors/9001
Get sensor history

curl http://localhost:3001/api/sensors/9001/history
Get all gates

curl http://localhost:3001/api/gates
Get specific gate

curl http://localhost:3001/api/gates/gate-001
📚 API Documentation
Base URL

http://localhost:3001/api
Endpoints
GET /health

Health check endpoint.

Response:

json

{

“status”: “healthy”,

“time”: “2026-01-01T10:30:00Z”

}
GET /api/sensors

Get all active sensors with their latest readings.

Response:

json

{

“sensors”: [

{

“id”: “9001”,

“type”: “soil-moisture”,

“value”: “45.2”,

“unit”: “%”,

“lat”: “35.72”,

“lon”: “51.33”,

“timestamp”: “2026-01-01T10:30:00Z”

}

]

}
GET /api/sensors/{id}

Get specific sensor’s latest reading.

Example:

bash

curl http://localhost:3001/api/sensors/9001

Response:

json

{

“id”: “9001”,

“type”: “soil-moisture”,

“value”: “45.2”,

“unit”: “%”,

“lat”: “35.72”,

“lon”: “51.33”,

“timestamp”: “2026-01-01T10:30:00Z”

}
GET /api/sensors/{id}/history

Get sensor’s historical data (last 100 readings).

Response:

json

{

“history”: [

{

“value”: 45.2,

“timestamp”: “2026-01-01T10:30:00Z”

}

]

}
GET /api/gates

Get all gates with their current status.

Response:

json

{

“gates”: [

{

“id”: “gate-001”,

“is_open”: “0”,

“timestamp”: “2026-01-01T10:30:00Z”

}

]

}
GET /api/gates/{id}

Get specific gate’s status.

Response:

json

{

“id”: “gate-001”,

“is_open”: “0”,

“timestamp”: “2026-01-01T10:30:00Z”

}
🐛 Troubleshooting
Issue: “Connection refused” when starting cloud server

Solution:

bash
Check if Redis and Mosquitto are running

docker-compose ps
If not running, start them

docker-compose up -d
Verify Redis is accessible

docker exec -it redis redis-cli ping
Issue: Dashboard shows no data

Checklist:

    ✅ Cloud server is running
    ✅ Simulator is running
    ✅ Check cloud server logs
    ✅ Refresh browser (Ctrl+F5)
    ✅ Check browser console (F12)

Debug Steps:

bash
Test if API returns data

curl http://localhost:3001/api/sensors
Check Redis directly

docker exec -it redis redis-cli KEYS “*”
Issue: Port already in use

Solution:

bash
On Linux/macOS

lsof -i :3001

kill -9 <PID>
On Windows

netstat -ano | findstr :3001

taskkill /PID <PID> /F
Issue: MQTT broker not accessible

Solution:

bash
Check if Mosquitto is running

docker ps | grep mosquitto
Check Mosquitto logs

docker logs mosquitto
Restart Mosquitto

docker-compose restart mosquitto
🛠️ Configuration
Environment Variables

Create .env file:

env
Redis Configuration

REDIS_HOST=localhost

REDIS_PORT=6379
MQTT Configuration

MQTT_BROKER=tcp://localhost:1883
Server Configuration

SERVER_PORT=3001
Simulator Configuration

Edit simulator/main.go:

go

// Change publish interval (line ~200)

time.Sleep(5 * time.Second) // Change from 2 to 5 seconds

// Add more sensors

var sensors = []Sensor{

{

ID: “9007”,

Type: “light”,

Unit: “lux”,

Min: 100.0,

Max: 1000.0,

Lat: 35.72,

Lon: 51.33,

},

}
📄 License

MIT License - Copyright © 2026
📞 Support

    🐛 Issues: GitHub Issues
    📖 Developer Docs: DEVELOPER.md

🗺️ Roadmap
Phase 1 (Complete)

    [x] MQTT pub/sub system
    [x] Redis data storage
    [x] REST API
    [x] Web dashboard

Phase 2 (Planned)

    [ ] IOTA integration
    [ ] User authentication
    [ ] Mobile app

⭐ If you find this project helpful, please give it a star on GitHub!

</details>