# GoStream

GoStream is a lightweight MP3 streaming server written in Go. It provides real-time audio streaming with synchronized playback across multiple clients.

## Features

- **Real-time MP3 streaming** - Continuous audio streaming over HTTP with chunked transfer encoding
- **Synchronized playback** - Multiple clients receive the same audio stream in sync
- **Playback modes** - Support for both random and sequential track playback
- **Skip/Next controls** - API endpoints to skip songs and preview the next track
- **Continuous hybrid audio pipeline** - Files and incompatible live sources use FFmpeg; matching MP3 live sources are passed through unchanged to avoid unnecessary quality loss
- **Stream metadata** - ID3 tag parsing for track title and artist information
- **Icecast compatibility** - Stats endpoint compatible with Icecast format for player integration
- **Icecast source input** - Accept live audio from DJ apps and other sources via Icecast protocol
- **Configurable gap/silence** - Set custom silence duration between songs (default 500ms)
- **CORS support** - Cross-Origin Resource Sharing enabled for browser-based streaming
- **Debug mode** - Enhanced logging for troubleshooting
- **Cross-platform support** - Runs on Windows, Linux, and macOS
- **Real-time client tracking** - Logs client connections and disconnections with request IDs

## Installation

### From Source

```bash
git clone https://github.com/script-php/GoStream.git
cd GoStream
go build
```

The binary will be created as `gostream` (or `gostream.exe` on Windows).

### Getting FFmpeg Binaries

**Important:** FFmpeg binaries are not included in the source code repository. You have two options:

1. **Use pre-built release package** (Recommended)
   - Download from [GitHub Releases](https://github.com/script-php/GoStream/releases)
   - Includes compiled binary + FFmpeg binaries + config

2. **Build from source and add FFmpeg**
   - After building, you need to obtain FFmpeg:
     - **Windows/Linux:** Download from [FFmpeg.org](https://ffmpeg.org/download.html) and place in `ffmpeg/` folder
     - **macOS/all platforms:** Install via system (e.g., `brew install ffmpeg`)

## Usage

### Basic Usage

```bash
./gostream -d /path/to/your/music
```

The server will start on port 8090 by default.

### Command Line Options

- `-d string` - Directory containing MP3 files (default: current directory)
- `-p int` - Server port number (default: 8090)
- `-host string` - Server host address (default: "0.0.0.0")
- `-r` - Enable random playback mode
- `-debug` - Enable debug mode for detailed logging
- `-n string` - Server name (default: "GoStream")
- `-gap int` - Gap/silence between songs in milliseconds (default: 500)
- `-icecast-source-port int` - Port for Icecast source client connections (default: 0 = disabled)
- `-c string` - Load configuration from JSON file or URL
- `-register` - Register as systemd service (requires sudo and -c flag) - Linux only
- `-h` - Show help information

### Configuration File

You can use a JSON configuration file to manage all settings. This is useful for:
- Remote server management
- Version control of configurations
- Easy switching between environments

#### Creating a Config File

Create a `config.json` file:

```json
{
  "port": 8090,
  "host": "0.0.0.0",
  "directory": "/path/to/music",
  "name": "My Radio Station",
  "random": false,
  "debug": false,
  "gap_ms": 500,
  "standard_bitrate": "128k",
  "standard_sample_rate": "44100",
  "cache_dir": ".cache",
  "cache_ttl_minutes": 10,
  "icecast_source_port": 8001,
  "username": "admin",
  "password": "change-me"
}
```

#### Using Local Config File

```bash
./gostream -c config.json
```

#### Using Remote Config File

```bash
./gostream -c https://mywebsite.com/config.json
```

#### Config File Options

- `port` (int) - Server port number
- `host` (string) - Server host address
- `directory` (string) - Path to music directory
- `name` (string) - Server name
- `random` (bool) - Enable random playback mode
- `debug` (bool) - Enable debug mode
- `gap_ms` (int) - Gap/silence between songs in milliseconds
- `standard_bitrate` (string) - Station output bitrate (e.g., "128k", "192k", "256k") - default: "128k"
- `standard_sample_rate` (string) - Station output sample rate (e.g., "44100", "48000") - default: "44100"
- `cache_dir` / `cache_ttl_minutes` - Deprecated legacy-reader compatibility options
- `icecast_source_port` (int) - Port for Icecast source client connections (0 = disabled) - default: 0
- `username` / `password` (string) - Credentials shared by administrative API endpoints and live source connections; set both or leave both empty

**Note:** Files are decoded to stereo PCM and encoded by the persistent station encoder. A live MP3 source bypasses FFmpeg only when its bitrate, sample rate, and channel count match the configured output. Different MP3 parameters and non-MP3 live formats are converted by FFmpeg. Switching sources does not close existing listener connections. Command-line arguments take precedence over config file values.

### Icecast Source Input (Live Audio)

GoStream supports accepting live audio from DJ applications and other sources via the **Icecast protocol**. This allows you to stream live audio without pre-recorded files.

#### Enabling Icecast Source Input

Add to your config file:

```json
{
  "port": 8090,
  "icecast_source_port": 8001
}
```

Or use command-line flag:

```bash
./gostream -icecast-source-port 8001
```

#### Connecting a Source

Use any Icecast-compatible source client to push audio to your server:

```bash
# Example with curl and ffmpeg
ffmpeg -i input.mp3 -f mp3 -b:a 128k - | \
  curl -X SOURCE \
  -u admin:password \
  -H "Content-Type: audio/mpeg" \
  --data-binary @- \
  http://localhost:8001/
```

#### Features

- Accept audio from DJ apps, ffmpeg, OBS Studio, and standard Icecast source clients
- Automatic failover to file playlist when source disconnects
- Broadcast live audio to all connected listeners
- Pass matching MP3 sources through unchanged and automatically convert incompatible MP3 or non-MP3 sources to the configured format
- Full HTTP compatibility
- Keep existing listener connections open while switching between files and live input

For detailed setup instructions and examples, see [ICECAST_SOURCE_GUIDE.md](release/ICECAST_SOURCE_GUIDE.md).

#### Testing Icecast Connection

Use the FFmpeg and `curl` example above, or configure an Icecast 2 source in Mixxx with mount `/live`, the source port, and the configured credentials.

### Legacy Cache Options

`cache_dir` and `cache_ttl_minutes` are retained for configuration compatibility
with the older frame reader. The continuous stream engine decodes source files
directly and does not create normalized file caches.

### Examples

```bash
# Basic usage with music directory
./gostream -d /music

# Custom port and random playback
./gostream -d /music -p 8080 -r

# Custom server name
./gostream -d /music -n "My Radio Station"

# Debug mode with custom gap between songs
./gostream -d /music -debug -gap 1000

# Using local config file
./gostream -c config.json

# Using remote config file
./gostream -c https://mywebsite.com/config.json

# Override config file with command-line options
./gostream -c config.json -p 8080 -r

# All features enabled (command-line)
./gostream -d /music -p 8080 -r -n "My Station" -debug -gap 500
```

### Systemd Service Registration (Linux)

GoStream can be registered as a systemd service to start automatically on boot and run continuously in the background.

#### Prerequisites

- Linux system with systemd
- GoStream binary compiled and available
- Root access (sudo) for installation

#### Register as Systemd Service

```bash
sudo ./gostream -c config.json -register
```

This command will:
- Validate the config file exists
- Create `/etc/systemd/system/gostream.service` 
- Enable the service on system boot
- Start the service immediately
- Display the service status and useful commands

#### Service Management

After registration, manage the service using standard systemd commands:

```bash
# Check service status
sudo systemctl status gostream

# Start/stop the service
sudo systemctl start gostream
sudo systemctl stop gostream

# Restart the service
sudo systemctl restart gostream

# View real-time logs
sudo journalctl -u gostream -f

# View recent logs
sudo journalctl -u gostream -n 50

# Check if service is enabled on boot
sudo systemctl is-enabled gostream

# Disable/enable on boot
sudo systemctl disable gostream
sudo systemctl enable gostream
```

#### Requirements

- The `-c` flag (config file) is mandatory when using `-register`
- Must run with `sudo` privileges
- The service will run as the user that executed the registration command

#### Example Service File

The registration creates a systemd service file at `/etc/systemd/system/gostream.service`:

```ini
[Unit]
Description=GoStream Server
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=your_username
WorkingDirectory=/path/to/gostream
ExecStart="/path/to/gostream" -c "/path/to/config.json"
Restart=always
RestartSec=5
StartLimitBurst=5

LimitNOFILE=65535
LimitNPROC=65535

StandardOutput=journal
StandardError=journal
SyslogIdentifier=gostream

[Install]
WantedBy=multi-user.target
```

#### Uninstalling the Service

To remove the systemd service:

```bash
sudo systemctl stop gostream
sudo systemctl disable gostream
sudo rm /etc/systemd/system/gostream.service
sudo systemctl daemon-reload
```

## API Endpoints

- `GET /` - Main MP3 audio stream
- `GET /stream.mp3` - MP3 audio stream (alternative URL for better player compatibility)
- `GET /info` - Server and current track information (JSON format)
- `GET /stats` - Icecast-compatible statistics endpoint
- `GET /skip` - Skip to next song and return now playing info
- `GET /next` - Get information about the next song
- `GET /status` - Get current stream status and now playing track
- `GET /metrics` - Detailed system and streaming metrics (memory, GC, bandwidth)
- `GET /songs` - List all available songs with their hash IDs

## API Response Examples

### Stream Information (`/info`)

```bash
curl http://localhost:8090/info
```

Response:

```json
{
  "success": true,
  "data": {
    "name": "GoStream",
    "version": "1.0.0",
    "time": 1234567890123,
    "FMInfo": {
      "title": "Song Title",
      "artist": "Artist Name",
      "sampleRate": "44100",
      "bitRate": "320",
      "filename": "song.mp3",
      "url": "/"
    }
  }
}
```

### Skip Song (`/skip`)

```bash
curl http://localhost:8090/skip
```

Response:

```json
{
  "status": "skipped",
  "now_playing": {
    "title": "Next Song.mp3",
    "artist": "Artist Name",
    "bitrate": "320",
    "samplerate": "44100"
  }
}
```

### Stream Status (`/status`)

```bash
curl http://localhost:8090/status
```

Response:

```json
{
  "status": "playing",
  "now_playing": {
    "title": "Current Song.mp3",
    "artist": "Artist Name",
    "bitrate": "320",
    "samplerate": "44100"
  }
}
```

### Next Song (`/next`)

```bash
curl http://localhost:8090/next
```

Response:

```json
{
  "status": "success",
  "next_song": {
    "title": "Next Song.mp3",
    "artist": "Artist Name",
    "bitrate": "320",
    "samplerate": "44100"
  }
}
```

### Icecast Stats (`/stats`)

```bash
curl http://localhost:8090/stats
```

Response (Icecast XML-compatible JSON):

```json
{
  "icestats": {
    "source": {
      "title": "Current Song.mp3",
      "artist": "Artist Name",
      "name": "GoStream",
      "description": "GoStream",
      "genre": "Stream",
      "bitrate": "320",
      "samplerate": "44100"
    }
  }
}
```

### System Metrics (`/metrics`)

```bash
curl http://localhost:8090/metrics
```

Response:

```json
{
  "status": "success",
  "metrics": {
    "active_listeners": 2,
    "total_data_streamed": {
      "bytes": 52428800,
      "human": "50.00 MB"
    },
    "stream_uptime": {
      "seconds": 1800,
      "formatted": "00:30:00"
    },
    "memory": {
      "current_usage_mb": 3,
      "heap_alloc_mb": 2,
      "heap_sys_mb": 5,
      "total_alloc_mb": 45,
      "sys_mb": 12
    },
    "garbage_collection": {
      "gc_runs": 15,
      "gc_pause_ms": "0.25 ms",
      "gc_pause_raw_ms": 0.25123
    },
    "system": {
      "goroutines": 12
    },
    "bandwidth": {
      "current_mbps": "0.26 Mbps",
      "raw_mbps": 0.256342134
    }
  }
}
```

#### Memory Metrics Explained

- **current_usage_mb** - Total heap memory allocated and in use
- **heap_alloc_mb** - Heap memory currently allocated
- **heap_sys_mb** - Heap memory from the system
- **total_alloc_mb** - Total heap memory allocated (cumulative, never decreases)
- **sys_mb** - Total memory obtained from system

#### Garbage Collection Metrics

- **gc_runs** - Number of GC cycles performed
- **gc_pause_ms** - Duration of the last GC pause (in milliseconds)

## Using the Stream

### Direct Stream Access

```bash
# Using curl
curl http://localhost:8090/ > output.mp3

# Using VLC
vlc http://localhost:8090/

# Using Windows Media Player
start http://localhost:8090/stream.mp3
```

### Getting Track Information

```bash
curl http://localhost:8090/info
```


## Requirements

- Go 1.20 or later
- MP3 files in the specified directory
- FFmpeg (either bundled in `ffmpeg/` folder OR available in system PATH)

FFmpeg is required for file playback and for live sources that cannot use direct MP3 passthrough:
- **Windows & Linux:** Bundled FFmpeg binaries are included in the `ffmpeg/` folder
- **macOS or custom:** Ensure FFmpeg is installed and available in system PATH
  - Install via Homebrew: `brew install ffmpeg`
  - Or download from: https://ffmpeg.org/download.html

## Optional Requirements

- Debug mode for detailed logging

## Dependencies

- Echo v4 - HTTP web framework
- mp3lib - MP3 file processing
- id3v2 - ID3 tag reading

This project is licensed under the MIT License.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request
