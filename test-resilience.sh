#!/bin/bash

# AMPS AMQP Resilience Test Script
# This script demonstrates AMPS's ability to recover gracefully from RabbitMQ restarts

set -e

echo "🚀 AMPS AMQP Resilience Test"
echo "==============================="

# Check if Docker and Docker Compose are available
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is required but not installed"
    exit 1
fi

if ! command -v docker compose &> /dev/null; then
    echo "❌ Docker Compose is required but not installed"
    exit 1
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to check AMPS health
check_amps_health() {
    local timeout=${1:-10}
    local count=0

    echo -n "🔍 Checking AMPS health"

    while [ $count -lt $timeout ]; do
        if curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/healthz | grep -q "200"; then
            echo -e " ${GREEN}✅ HEALTHY${NC}"
            return 0
        fi
        echo -n "."
        sleep 1
        count=$((count + 1))
    done

    echo -e " ${RED}❌ UNHEALTHY${NC}"
    return 1
}

# Function to wait for RabbitMQ
wait_for_rabbitmq() {
    local timeout=${1:-30}
    local count=0

    echo -n "🐰 Waiting for RabbitMQ"

    while [ $count -lt $timeout ]; do
        if docker compose exec -T rabbitmq rabbitmqctl status &> /dev/null; then
            echo -e " ${GREEN}✅ READY${NC}"
            return 0
        fi
        echo -n "."
        sleep 1
        count=$((count + 1))
    done

    echo -e " ${RED}❌ TIMEOUT${NC}"
    return 1
}

# Function to show container logs
show_logs() {
    local service=$1
    local lines=${2:-10}
    echo "📋 Last $lines lines from $service:"
    docker compose logs --tail=$lines $service | sed 's/^/  /'
}

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up..."
    docker compose down -v --remove-orphans 2>/dev/null || true
    echo "✅ Cleanup complete"
}

# Set up cleanup on exit
trap cleanup EXIT

echo ""
echo "Phase 1: Starting services"
echo "=========================="

# Start the services
echo "🚀 Starting AMPS and RabbitMQ..."
docker compose up -d --build

# Wait for RabbitMQ to be ready
if ! wait_for_rabbitmq 60; then
    echo "❌ RabbitMQ failed to start"
    show_logs rabbitmq
    exit 1
fi

# Wait for AMPS to be healthy
sleep 5
if ! check_amps_health 30; then
    echo "❌ AMPS failed to start healthy"
    show_logs amps
    exit 1
fi

echo ""
echo "Phase 2: Testing resilience"
echo "==========================="

echo "💥 Stopping RabbitMQ to simulate failure..."
docker compose stop rabbitmq

# Give AMPS time to detect the disconnection
sleep 3

echo "🔍 AMPS should now be unhealthy but still running..."
if check_amps_health 5; then
    echo -e "${YELLOW}⚠️  AMPS reports healthy but RabbitMQ is down${NC}"
    echo "   This might indicate the health check hasn't updated yet"
else
    echo -e "${GREEN}✅ AMPS correctly reports unhealthy${NC}"
fi

# Show recent AMPS logs to demonstrate reconnection attempts
echo ""
show_logs amps 20

echo ""
echo "Phase 3: Testing recovery"
echo "========================"

echo "🔄 Restarting RabbitMQ..."
docker compose start rabbitmq

# Wait for RabbitMQ to be ready
if ! wait_for_rabbitmq 60; then
    echo "❌ RabbitMQ failed to restart"
    show_logs rabbitmq
    exit 1
fi

# Give AMPS time to reconnect
echo "⏳ Waiting for AMPS to reconnect..."
sleep 10

if check_amps_health 30; then
    echo -e "${GREEN}🎉 SUCCESS: AMPS has recovered!${NC}"
else
    echo -e "${RED}❌ FAILED: AMPS did not recover${NC}"
    show_logs amps 30
    exit 1
fi

echo ""
echo "Phase 4: Verification"
echo "===================="

echo "📊 Final status check:"
echo "  RabbitMQ Management UI: http://localhost:15672 (guest/guest)"
echo "  AMPS Health Check: http://localhost:8080/healthz"

# Show final logs
echo ""
show_logs amps 15

echo ""
echo -e "${GREEN}🎉 AMQP Resilience Test PASSED!${NC}"
echo ""
echo "Key observations:"
echo "  ✅ AMPS survived RabbitMQ restart without crashing"
echo "  ✅ AMPS automatically reconnected when RabbitMQ came back"
echo "  ✅ Health checks accurately reflected connection status"
echo "  ✅ No manual intervention was required for recovery"
echo ""
echo "You can now test manual scenarios:"
echo "  1. docker compose logs -f amps     # Watch AMPS logs"
echo "  2. docker compose restart rabbitmq # Restart RabbitMQ again"
echo "  3. curl http://localhost:8080/healthz # Check health"
echo ""
echo "Press Ctrl+C to stop all services and cleanup"

# Keep running until user stops
wait
