# Synology Download Station Status Monitor

This is a Go application that monitors the status of downloads in Synology Download Station.

## Prerequisites

- Go 1.21 or higher installed on your system
- Access to a Synology NAS with Download Station enabled

## Setup Instructions

1. **Install Go** (if not already installed):
   - Visit https://golang.org/doc/install for installation instructions
   - Follow the appropriate steps for your operating system

2. **Set Environment Variables**:
   Export the following environment variables:
   ```bash
   export SYNOLOGY_HOST="192.168.1.34"
   export SYNOLOGY_PORT="5000"
   export SYNOLOGY_USERNAME="your_username"
   export SYNOLOGY_PASSWORD="your_password"
   ```

   Or set them inline when running:
   ```bash
   SYNOLOGY_HOST="192.168.1.34" SYNOLOGY_PORT="5000" SYNOLOGY_USERNAME="your_username" SYNOLOGY_PASSWORD="your_password" go run main.go
   ```

3. **Run the Application**:
   ```bash
   go run main.go
   ```

## Configuration

- Default host: `192.168.1.34`
- Default port: `5000`
- Username and password must be provided via environment variables
- These values can be overridden by setting the corresponding environment variables

## Building the Project

To build a standalone binary:
```bash
go build -o synology-status main.go
```

Then run with:
```bash
./synology-status
```

## How It Works

This application connects to your Synology DSM (DiskStation Manager) using the REST API:
1. Authenticates with your credentials
2. Retrieves download tasks from Download Station
3. Displays task information including:
   - Task name
   - Status
   - Size in GB
   - Download duration
   - Average download speed in MB/s