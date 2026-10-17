# Icecast Source Input Guide

## Overview

GoStream now supports **Icecast source client connections**, allowing you to stream audio directly from DJ applications, audio sources, or other streaming tools into GoStream. This enables real-time live streaming scenarios where external sources push audio to your server.

## How It Works

When you enable the Icecast source server:

1. DJ apps or audio sources connect as **source clients** to your GoStream server
2. They send audio data via the **Icecast protocol** (HTTP-based with audio payload)
3. The audio is buffered and made available for streaming to listeners
4. Listeners can connect to the main stream endpoint and receive the audio

## Configuration

### Using Config File

Add `icecast_source_port` to your configuration JSON:

```json
{
  "port": 8090,
  "host": "0.0.0.0",
  "directory": "./music",
  "name": "My Radio Station",
  "icecast_source_port": 8001,
  "username": "admin",
  "password": "password"
}
```

Then run:
```bash
./GoStream -c config.json
```

### Using Command Line

```bash
./GoStream -icecast-source-port 8001
```

### Disabling Icecast Input

Set `icecast_source_port` to `0` or leave it out of the config (default is disabled):

```bash
# This will run without Icecast source input
./GoStream
```

## Connecting Source Clients

### General Requirements

Source clients must send:

1. **HTTP-like headers** with at least a `Content-Type` header specifying the audio format
2. **Audio data** following the headers

### Supported Formats

GoStream inspects an MP3 source's bitrate, sample rate, and channel count. When
all three match the configured station output, its MP3 bytes are forwarded
unchanged. A mismatched MP3 source is decoded and re-encoded by FFmpeg. Other
audio formats such as AAC, WAV, and OGG are also converted when the installed
FFmpeg build supports them.

If `username` and `password` are configured, source clients must use HTTP Basic
Authentication with those credentials. Both values must be configured together.
Basic Authentication is not encrypted, so expose the source port only through a
trusted network, firewall/VPN, or TLS proxy.

### Example Source Connection (Using curl)

```bash
# Stream MP3 file from local file
curl -X SOURCE \
  -u admin:password \
  -H "Content-Type: audio/mpeg" \
  -H "ice-name: My DJ Show" \
  -H "ice-genre: Electronic" \
  --data-binary @audio.mp3 \
  http://localhost:8001/

# Or stream continuous audio (ffmpeg example)
ffmpeg -i input.mp3 -f mp3 -c:a libmp3lame -ab 128k - | \
  curl -X SOURCE \
  -u admin:password \
  -H "Content-Type: audio/mpeg" \
  -H "ice-name: Live DJ Stream" \
  --data-binary @- \
  http://localhost:8001/
```

### Using Popular DJ/Streaming Apps

#### OBS Studio

GoStream does not accept RTMP or RTMPS directly. Route OBS audio through a tool
that can publish an Icecast-compatible HTTP source, such as FFmpeg.

#### WinAmp / Shoutcast Plugin
1. Configure plugin to stream to:
   - Host: your_server_ip
   - Port: 8001
   - Content-Type: audio/mpeg
   
#### FFmpeg (Generic)

Stream from any source to GoStream:

```bash
# From microphone
ffmpeg -f dshow -i "your_microphone" -f mp3 - | \
  curl -X SOURCE \
  -u admin:password \
  -H "Content-Type: audio/mpeg" \
  --data-binary @- \
  http://localhost:8001/

# From USB audio device
ffmpeg -f dshow -i "your_usb_device" -f mp3 -b:a 128k - | \
  curl -X SOURCE \
  -u admin:password \
  -H "Content-Type: audio/mpeg" \
  --data-binary @- \
  http://localhost:8001/

# From another stream
ffmpeg -i http://example.com/stream.mp3 \
  -f mp3 -b:a 128k - | \
  curl -X SOURCE \
  -u admin:password \
  -H "Content-Type: audio/mpeg" \
  --data-binary @- \
  http://localhost:8001/
```

## Monitoring Icecast Connections

### Check if Source is Active

The server automatically logs when:
- A source client connects
- Connection is accepted/rejected
- Audio data is being received
- Connection is dropped

Check logs:
```bash
./GoStream -debug  # Enable debug logging
```

### API Endpoint

You can check server status via:

```bash
curl http://localhost:8090/info
curl http://localhost:8090/stats
```

## Important Notes

### Multiple Simultaneous Sources

- **One active source at a time** - If a new authenticated source connects while one is active, the previous connection is closed and replaced
- **No queue** - Only the most recent source is used

### Audio Buffering

- Audio is buffered in memory using a capacity derived from the configured 512 KiB source buffer
- If buffer fills up, oldest chunks are dropped
- This prevents memory overflow on slow listeners

### Bandwidth & Protocol

- Uses standard TCP connections
- No actual Icecast server running (pure Go implementation)
- Compatible with standard Icecast source client protocols
- HTTP/1.0 compatible

### Metadata

- Basic metadata is parsed from headers (`ice-name`, `ice-genre`, `ice-url`, etc.)
- Metadata is available via the server info endpoints
- Real-time metadata updates are not yet supported (use file-based metadata)

## Troubleshooting

### Source won't connect

1. **Check port is open**: `netstat -an | grep 8001`
2. **Check firewall**: Ensure the port isn't blocked
3. **Verify Content-Type**: Must be `audio/*`
4. **Check server logs**: Run with `-debug` flag

### Audio sounds distorted

1. Check the log for either `Live MP3 passthrough started` or `FFmpeg transcoding enabled`
2. Confirm the source network connection is stable and is not exhausting the source buffer
3. Test the same source in direct MP3 passthrough mode to separate transport problems from transcoding problems

### Connection drops

1. **Check network stability**: Monitor packet loss
2. **Increase buffer**: Audio buffer is pre-sized; network issues may cause drops
3. **Check firewall**: Some firewalls timeout idle connections

## Hybrid Operation

You can run both file-based and source-based streaming:

1. **Source connected**: Listeners receive source audio
2. **Source disconnected**: GoStream automatically falls back to file playlist
3. **Source reconnects**: Switches back to live source

Set `directory` in config to maintain the playlist as a fallback.

The listener-facing HTTP connection remains open during every transition. The
persistent station encoder continues across file and transcoded-live modes;
matching live MP3 temporarily bypasses that encoder. A short silence fill is
used while a decoder becomes ready.

## Performance Considerations

- **Hybrid output**: Matching MP3 is passed through; all other inputs use the configured FFmpeg output
- **Memory usage**: ~512KB base + audio buffer
- **Network**: Bandwidth depends on source bitrate (128k = ~16KB/s)

## Future Enhancements

Potential additions:
- [ ] Multiple simultaneous sources with mixing
- [ ] Real-time metadata updates (ICY metadata)
- [ ] Buffer statistics and monitoring

## See Also

- [CONFIG_GUIDE.md](CONFIG_GUIDE.md) - Full configuration reference
- GoStream README - General information
