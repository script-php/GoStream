# GoStream

GoStream is a lightweight MP3 streaming server written in Go. It provides real-time audio streaming with synchronized playback across multiple clients.

## Features

- **Real-time MP3 streaming** - Continuous audio streaming over HTTP with chunked transfer encoding
- **Synchronized playback** - Multiple clients receive the same audio stream in sync
- **Playback modes** - Support for both random and sequential track playback
- **Skip/Next controls** - API endpoints to skip songs and preview the next track
- **Audio normalization** - Optional FFmpeg-based audio normalization to standardized bitrate and sample rate
- **Stream metadata** - ID3 tag parsing for track title and artist information
- **Icecast compatibility** - Stats endpoint compatible with Icecast format for player integration
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
- `-normalize` - Enable audio normalization to standard bitrate/sample rate using FFmpeg
- `-c string` - Load configuration from JSON file or URL
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
  "normalize": false,
  "standard_bitrate": "128k",
  "standard_sample_rate": "44100",
  "cache_dir": ".cache",
  "cache_ttl_minutes": 10
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
- `normalize` (bool) - Enable audio normalization
- `standard_bitrate` (string) - Bitrate for normalized audio (e.g., "128k", "192k", "256k") - default: "128k"
- `standard_sample_rate` (string) - Sample rate for normalized audio (e.g., "44100", "48000") - default: "44100"
- `cache_dir` (string) - Directory to store cached normalized files - default: ".cache"
- `cache_ttl_minutes` (int) - Cache time-to-live in minutes (files older than this are deleted, 0 = no cleanup) - default: 10

**Note:** Command-line arguments take precedence over config file values.

### Cache Management

GoStream includes automatic cache cleanup to prevent unlimited disk usage when audio normalization is enabled.

#### How Cache Cleanup Works

- **Automatic Cleanup** - Runs every 5 minutes to remove expired cache files
- **Time-Based Expiry** - Files older than `cache_ttl_minutes` are automatically deleted
- **Default TTL** - 10 minutes (configurable)
- **Intelligent Logging** - Shows when and how much space is freed

#### Configuration Examples

```json
{
  "normalize": true,
  "cache_ttl_minutes": 10,
  "cache_dir": ".cache"
}
```

#### Cache Cleanup Scenarios

| TTL Setting | Behavior | Use Case |
|---|---|---|
| `cache_ttl_minutes: 0` | Cleanup disabled | Persistent cache, manual removal |
| `cache_ttl_minutes: 5` | Files expire after 5 min | Low disk space scenarios |
| `cache_ttl_minutes: 10` | Files expire after 10 min | **Default, recommended** |
| `cache_ttl_minutes: 60` | Files expire after 1 hour | High performance, more disk usage |

**Example Log Output:**
```
Cache cleanup routine started (TTL: 10 minutes, check interval: 5 minutes)
Cache cleanup: Deleted 3 files, freed 8.45 MB
```

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

# Enable audio normalization
./gostream -d /music -normalize

# Using local config file
./gostream -c config.json

# Using remote config file
./gostream -c https://mywebsite.com/config.json

# Override config file with command-line options
./gostream -c config.json -p 8080 -r

# All features enabled (command-line)
./gostream -d /music -p 8080 -r -n "My Station" -debug -gap 500 -normalize
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
- `GET /songs` - List all available songs with their index numbers

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

## Docker Usage
eployment

### Running Directly

The compiled binary can be run directly on any system with the required dependencies:

```bash
./gostream -c config.json
```

### Docker Deployment

If you want to deploy GoStream in Docker, you can create your own Dockerfile:

```dockerfile
FROM golang:1.20 AS builder
WORKDIR /src
COPY . .
RUN go build -o gostream .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /src/gostream /app/
VOLUME ["/music"]
EXPOSE 8090
ENTRYPOINT ["/app/gostream"]
CMD ["-d", "/music"]
```

Build and run:

```bash
docker build -t gostream .
docker run -d -p 8090:8090 -v /path/to/music:/music gostream

## Requirements

- Go 1.20 or later
- MP3 files in the specified directory
- FFmpeg (bundled for Windows and Linux, or available in system PATH)

## Optional Requirements

- FFmpeg - For audio normalization feature (included as bundled binaries for Windows and Linux)

## Dependencies

- Echo v4 - HTTP web framework
- mp3lib - MP3 file processing
- id3v2 - ID3 tag reading

## License

This project is licensed under the MIT License.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request