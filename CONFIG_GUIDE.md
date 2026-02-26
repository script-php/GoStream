# Configuration Guide

This guide explains all available configuration options for GoStream.

## Configuration File

Configuration is loaded from `config.json` in the application root directory. Use `config.example.json` as a template.

Example usage:
```bash
./gostream -c config.json
```

---

## Server Settings

### port
- **Type**: `int`
- **Default**: `8090`
- **Description**: The port number the server listens on
- **Example**: `"port": 8090`

### host
- **Type**: `string`
- **Default**: `"0.0.0.0"`
- **Description**: The host address to bind to. Use `0.0.0.0` for all interfaces or `127.0.0.1` for localhost only
- **Example**: `"host": "0.0.0.0"`

### debug
- **Type**: `boolean`
- **Default**: `false`
- **Description**: Enable debug mode for verbose logging
- **Example**: `"debug": true`

---

## Stream Settings

### name
- **Type**: `string`
- **Default**: `"GoStream"`
- **Description**: Your radio station name. Sent to clients in Shoutcast headers (icy-name)
- **Example**: `"name": "My Radio Station"`

### directory
- **Type**: `string`
- **Default**: Current working directory
- **Description**: Path to the folder containing MP3 files
- **Example**: `"directory": "./music"`

### random
- **Type**: `boolean`
- **Default**: `false`
- **Description**: Enable random playback. If false, plays songs in alphabetical order
- **Example**: `"random": true`

### gap_ms
- **Type**: `int`
- **Default**: `500`
- **Description**: Silence/gap between songs in milliseconds
- **Example**: `"gap_ms": 500`

---

## Audio Normalization

### normalize
- **Type**: `boolean`
- **Default**: `false`
- **Description**: Enable audio normalization using FFmpeg. Converts all audio to standard bitrate/sample rate for consistent streaming
- **Example**: `"normalize": true`

### standard_bitrate
- **Type**: `string`
- **Default**: `"128k"`
- **Description**: Target bitrate for normalized audio. Used when `normalize` is true
- **Example**: `"standard_bitrate": "128k"`

### standard_sample_rate
- **Type**: `string`
- **Default**: `"44100"`
- **Description**: Target sample rate for normalized audio in Hz. Used when `normalize` is true
- **Example**: `"standard_sample_rate": "44100"`

### cache_dir
- **Type**: `string`
- **Default**: `".cache"`
- **Description**: Directory where normalized audio files are cached
- **Example**: `"cache_dir": ".cache"`

### cache_ttl_minutes
- **Type**: `int`
- **Default**: `10`
- **Description**: Cache expiration time in minutes. Set to 0 to disable cache cleanup
- **Example**: `"cache_ttl_minutes": 60`

---

## Shoutcast Metadata

### genre
- **Type**: `string`
- **Default**: `""`
- **Description**: Radio station genre. Sent to clients in Shoutcast headers (icy-genre)
- **Example**: `"genre": "Hip-Hop"`

### url
- **Type**: `string`
- **Default**: `""`
- **Description**: Your website or station URL. Sent to clients in Shoutcast headers (icy-url)
- **Example**: `"url": "https://example.com"`

### notice1
- **Type**: `string`
- **Default**: `""`
- **Description**: First notice line. Sent to clients in Shoutcast headers (icy-notice1). Can include HTML
- **Example**: `"notice1": "<BR>This is a test stream<BR>"`

### notice2
- **Type**: `string`
- **Default**: `""`
- **Description**: Second notice line. Sent to clients in Shoutcast headers (icy-notice2). Can include HTML
- **Example**: `"notice2": "Enjoy the music!<BR>"`

### meta_interval
- **Type**: `int`
- **Default**: `8192`
- **Description**: Metadata update interval in bytes. Controls how often song information is sent to clients that request it (Icy-MetaData: 1 header). Standard value is 8192
- **Example**: `"meta_interval": 8192`

---

## Complete Example

```json
{
  "port": 8090,
  "host": "0.0.0.0",
  "directory": "./music",
  "name": "My Radio Station",
  "random": true,
  "debug": false,
  "gap_ms": 500,
  "normalize": true,
  "standard_bitrate": "128k",
  "standard_sample_rate": "44100",
  "cache_dir": ".cache",
  "cache_ttl_minutes": 60,
  "genre": "Various",
  "url": "https://example.com",
  "notice1": "Stream powered by GoStream<BR>",
  "notice2": "Enjoy the music!<BR>",
  "meta_interval": 8192
}
```

---

## Notes

- All paths can be relative (from the application root) or absolute
- String values are case-sensitive
- Boolean values must be `true` or `false` (lowercase)
- Integers must not be quoted
- The configuration file is validated on startup - invalid JSON will cause an error

## API Endpoints

Once configured and running, GoStream provides several endpoints:

- `/fm` - Stream endpoint (audio/mpeg)
- `/server-info` - Server information and current song
- `/stats` - Stream statistics (Icecast compatible format)
- `/status` - Current stream status
- `/skip` - Skip to next song
- `/next` - Get next song information
