package modules

import (
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	pcmChannels       = 2
	pcmBytesPerSample = 4 // 32-bit float preserves MP3 decoder overshoot
	pcmTickDuration   = 20 * time.Millisecond
	livePrebufferTime = 250 * time.Millisecond
	liveUnderrunWait  = 80 * time.Millisecond
	encoderHeadroom   = "-2dB"
)

// ContinuousStreamEngine owns the station's persistent file/transcoding MP3
// encoder. Matching live MP3 can bypass it; all other inputs feed PCM through
// it while the listener-facing HTTP connection remains open.
type ContinuousStreamEngine struct {
	ffmpegPath string
	sampleRate int
	bitrateBps int

	encoder      *exec.Cmd
	encoderInput io.WriteCloser
	encoderOut   io.ReadCloser
	encoderDone  chan struct{}
	failureCh    chan error

	stopCh chan struct{}
	skipCh chan struct{}
	stop   sync.Once
	wg     sync.WaitGroup

	publishMu          sync.Mutex
	publishRolling     []byte
	publishOrder       int
	liveMP3Passthrough atomic.Bool
}

var StreamEngine *ContinuousStreamEngine

type pcmProcess struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	chunks   chan []byte
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

type pcmSourceState struct {
	process        *pcmProcess
	queued         []byte
	closed         bool
	prebufferBytes int
	ready          bool
}

func configuredSampleRate() int {
	rate, err := strconv.Atoi(Config.StandardSampleRate)
	if err != nil || rate <= 0 {
		return 44100
	}
	return rate
}

func configuredBitrateBps() int {
	value := strings.ToLower(strings.TrimSpace(Config.StandardBitrate))
	multiplier := 1
	if strings.HasSuffix(value, "k") {
		multiplier = 1000
		value = strings.TrimSuffix(value, "k")
	}
	bitrate, err := strconv.Atoi(value)
	if err != nil || bitrate <= 0 {
		return 128000
	}
	return bitrate * multiplier
}

func NewContinuousStreamEngine() (*ContinuousStreamEngine, error) {
	ffmpegPath, err := GetFFmpegPath()
	if err != nil {
		return nil, err
	}
	return &ContinuousStreamEngine{
		ffmpegPath:  ffmpegPath,
		sampleRate:  configuredSampleRate(),
		bitrateBps:  configuredBitrateBps(),
		stopCh:      make(chan struct{}),
		skipCh:      make(chan struct{}, 1),
		encoderDone: make(chan struct{}),
		failureCh:   make(chan error, 1),
	}, nil
}

func (engine *ContinuousStreamEngine) Start() error {
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "f32le",
		"-ar", strconv.Itoa(engine.sampleRate),
		"-ac", strconv.Itoa(pcmChannels),
		"-i", "pipe:0",
		"-map_metadata", "-1",
		"-af", "volume=" + encoderHeadroom,
		"-c:a", "libmp3lame",
		"-b:a", Config.StandardBitrate,
		"-ar", strconv.Itoa(engine.sampleRate),
		"-ac", strconv.Itoa(pcmChannels),
		"-reservoir", "0",
		"-write_xing", "0",
		"-id3v2_version", "0",
		"-flush_packets", "1",
		"-f", "mp3", "pipe:1",
	}

	engine.encoder = exec.Command(engine.ffmpegPath, args...)
	input, err := engine.encoder.StdinPipe()
	if err != nil {
		return fmt.Errorf("create encoder input: %w", err)
	}
	output, err := engine.encoder.StdoutPipe()
	if err != nil {
		input.Close()
		return fmt.Errorf("create encoder output: %w", err)
	}
	engine.encoder.Stderr = io.Discard
	if err := engine.encoder.Start(); err != nil {
		input.Close()
		output.Close()
		return fmt.Errorf("start persistent MP3 encoder: %w", err)
	}
	engine.encoderInput = input
	engine.encoderOut = output

	engine.wg.Add(3)
	go func() {
		defer engine.wg.Done()
		err := engine.encoder.Wait()
		close(engine.encoderDone)
		select {
		case <-engine.stopCh:
			return
		default:
		}
		if err == nil {
			err = fmt.Errorf("persistent MP3 encoder exited unexpectedly")
		} else {
			err = fmt.Errorf("persistent MP3 encoder exited: %w", err)
		}
		select {
		case engine.failureCh <- err:
		default:
		}
	}()
	go func() {
		defer engine.wg.Done()
		engine.publishEncoderOutput()
	}()
	go func() {
		defer engine.wg.Done()
		engine.runSourceSelector()
	}()

	Logger.Info(fmt.Sprintf("Continuous stream encoder started (%s, %d Hz, stereo)", Config.StandardBitrate, engine.sampleRate))
	return nil
}

// Failure reports a fatal stream-engine failure. The application should exit
// so its process supervisor can restart the complete audio pipeline cleanly.
func (engine *ContinuousStreamEngine) Failure() <-chan error {
	return engine.failureCh
}

func (engine *ContinuousStreamEngine) Stop() {
	engine.stop.Do(func() {
		close(engine.stopCh)
		if engine.encoderInput != nil {
			engine.encoderInput.Close()
		}
		if engine.encoder != nil && engine.encoder.Process != nil {
			engine.encoder.Process.Kill()
		}
	})
	engine.wg.Wait()
}

func (engine *ContinuousStreamEngine) RequestSkip() {
	select {
	case engine.skipCh <- struct{}{}:
	default:
	}
}

func (engine *ContinuousStreamEngine) startPCMProcess(inputPath string, live bool) (*pcmProcess, error) {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if live {
		args = append(args, "-i", "pipe:0")
	} else {
		args = append(args, "-i", inputPath)
	}
	args = append(args,
		"-vn", "-map_metadata", "-1",
		"-f", "f32le", "-c:a", "pcm_f32le",
		"-ar", strconv.Itoa(engine.sampleRate),
		"-ac", strconv.Itoa(pcmChannels),
		"pipe:1",
	)

	process := &pcmProcess{
		cmd:    exec.Command(engine.ffmpegPath, args...),
		chunks: make(chan []byte, 64),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	process.cmd.Stderr = io.Discard

	if live {
		input, err := process.cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		process.input = input
	}
	output, err := process.cmd.StdoutPipe()
	if err != nil {
		if process.input != nil {
			process.input.Close()
		}
		return nil, err
	}
	if err := process.cmd.Start(); err != nil {
		if process.input != nil {
			process.input.Close()
		}
		output.Close()
		return nil, err
	}

	go func() {
		defer close(process.chunks)
		buffer := make([]byte, 16*1024)
		for {
			n, readErr := output.Read(buffer)
			if n > 0 {
				chunk := append([]byte(nil), buffer[:n]...)
				select {
				case process.chunks <- chunk:
				case <-process.stopCh:
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	go func() {
		process.cmd.Wait()
		close(process.doneCh)
	}()

	return process, nil
}

func (process *pcmProcess) Stop() {
	if process == nil {
		return
	}
	process.stopOnce.Do(func() {
		close(process.stopCh)
		if process.input != nil {
			process.input.Close()
		}
		if process.cmd.Process != nil {
			process.cmd.Process.Kill()
		}
	})
	select {
	case <-process.doneCh:
	case <-time.After(time.Second):
	}
}

func (state *pcmSourceState) take(size int, wait time.Duration) ([]byte, bool) {
	if state == nil || state.process == nil {
		return make([]byte, size), true
	}

	target := size
	if !state.ready && state.prebufferBytes > target {
		target = state.prebufferBytes
	}

	var timer *time.Timer
	var timeout <-chan time.Time
	if wait > 0 {
		timer = time.NewTimer(wait)
		timeout = timer.C
		defer timer.Stop()
	}

fill:
	for len(state.queued) < target && !state.closed {
		if wait == 0 {
			select {
			case chunk, ok := <-state.process.chunks:
				if !ok {
					state.closed = true
					break fill
				}
				state.queued = append(state.queued, chunk...)
			default:
				break fill
			}
			continue
		}

		select {
		case chunk, ok := <-state.process.chunks:
			if !ok {
				state.closed = true
				break
			}
			state.queued = append(state.queued, chunk...)
		case <-timeout:
			wait = 0
		}
	}

	// A live decoder writes PCM in bursts rather than one exact 20 ms block at
	// a time. Keep initial silence until the jitter buffer is ready instead of
	// consuming a partial burst and inserting an audible zero-filled gap.
	if !state.ready && state.prebufferBytes > 0 && len(state.queued) < state.prebufferBytes && !state.closed {
		return make([]byte, size), false
	}
	state.ready = true

	// Never splice silence after a partial PCM read. Pipe reads may stop in the
	// middle of a float sample; copying those bytes and padding the rest with
	// zeroes creates a malformed sample that is heard as a sharp scratch. Keep
	// partial output queued until a complete tick is available instead.
	if len(state.queued) < size && !state.closed {
		return make([]byte, size), false
	}

	result := make([]byte, size)
	copied := copy(result, state.queued)
	state.queued = state.queued[copied:]
	return result, state.closed && len(state.queued) == 0
}

func (engine *ContinuousStreamEngine) setLiveMode(enabled bool) {
	MusicReader.Lock.Lock()
	MusicReader.IsIcecastMode = enabled
	MusicReader.Lock.Unlock()
}

func (engine *ContinuousStreamEngine) startFileSource() (*pcmSourceState, error) {
	path, err := MusicReader.PrepareNextMusic()
	if err != nil {
		return nil, err
	}
	process, err := engine.startPCMProcess(path, false)
	if err != nil {
		return nil, err
	}
	if info := MusicReader.GetMusicInfoStoreData(); info != nil {
		info.SampleRate = strconv.Itoa(engine.sampleRate)
		info.BitRate = strconv.Itoa(engine.bitrateBps / 1000)
		MusicReader.SetInfoStoreData(*info)
	}
	Logger.Info(fmt.Sprintf("File PCM source started: %s", path))
	return &pcmSourceState{process: process, ready: true}, nil
}

func (engine *ContinuousStreamEngine) startLiveSource(generation uint64) (*pcmSourceState, error) {
	process, err := engine.startPCMProcess("", true)
	if err != nil {
		return nil, err
	}
	MusicReader.SetInfoStoreData(IMusicInfoStoreData{
		Title:      "Live Stream",
		Artist:     "Live",
		Filename:   "Live Stream",
		SampleRate: strconv.Itoa(engine.sampleRate),
		BitRate:    strconv.Itoa(engine.bitrateBps / 1000),
	})

	go func() {
		defer process.input.Close()
		for IcecastSource != nil && IcecastSource.HasActiveSource() && IcecastSource.GetSourceGeneration() == generation {
			chunk, ok := IcecastSource.GetAudioChunk()
			if !ok {
				continue
			}
			if _, err := process.input.Write(chunk); err != nil {
				return
			}
		}
	}()

	Logger.Info("Live PCM decoder started")
	prebufferBytes := engine.sampleRate * pcmChannels * pcmBytesPerSample * int(livePrebufferTime/time.Millisecond) / 1000
	return &pcmSourceState{process: process, prebufferBytes: prebufferBytes}, nil
}

func (engine *ContinuousStreamEngine) publishUnit(unit []byte, timeout int) {
	if len(unit) == 0 {
		return
	}
	const rollingSize = 64 * 1024

	engine.publishMu.Lock()
	defer engine.publishMu.Unlock()
	engine.publishRolling = append(engine.publishRolling, unit...)
	if len(engine.publishRolling) > rollingSize {
		engine.publishRolling = append([]byte(nil), engine.publishRolling[len(engine.publishRolling)-rollingSize:]...)
	}
	engine.publishOrder++
	MusicReader.SetBufferStoreData(IMusicReaderStoreData{
		InitialBuffer: append([]byte(nil), engine.publishRolling...),
		UnitBuffer:    append([]byte(nil), unit...),
		Timeout:       timeout,
		Order:         engine.publishOrder,
	})
}

// startLiveMP3Passthrough forwards the exact MP3 bytes supplied by Mixxx. It
// publishes at most five times per second; the bounded listener history keeps
// every published unit available to clients that are briefly behind.
func (engine *ContinuousStreamEngine) startLiveMP3Passthrough(generation uint64) {
	engine.liveMP3Passthrough.Store(true)
	MusicReader.SetInfoStoreData(IMusicInfoStoreData{
		Title:    "Live Stream",
		Artist:   "Live",
		Filename: "Live Stream",
	})

	go func() {
		const publishInterval = 200 * time.Millisecond
		lastPublish := time.Now()
		var pending []byte

		flush := func() {
			if len(pending) == 0 {
				return
			}
			engine.publishUnit(pending, 10)
			pending = nil
			lastPublish = time.Now()
		}
		defer flush()

		for IcecastSource != nil && IcecastSource.HasActiveSource() && IcecastSource.GetSourceGeneration() == generation {
			chunk, ok := IcecastSource.GetAudioChunk()
			if ok {
				pending = append(pending, chunk...)
			}
			if time.Since(lastPublish) >= publishInterval {
				flush()
			}
		}
	}()

	Logger.Info("Live MP3 passthrough started (FFmpeg bypassed)")
}

func (engine *ContinuousStreamEngine) runSourceSelector() {
	ticker := time.NewTicker(pcmTickDuration)
	defer ticker.Stop()

	bytesPerTick := engine.sampleRate * pcmChannels * pcmBytesPerSample * int(pcmTickDuration/time.Millisecond) / 1000
	var source *pcmSourceState
	mode := ""
	var liveGeneration uint64
	var gapUntil time.Time
	var retryAfter time.Time

	stopSource := func() {
		if source != nil {
			source.process.Stop()
			source = nil
		}
	}
	defer stopSource()

	for {
		select {
		case <-engine.stopCh:
			return
		case <-engine.skipCh:
			if mode == "file" {
				stopSource()
				gapUntil = time.Time{}
			}
		case now := <-ticker.C:
			liveActive := IcecastSource != nil && IcecastSource.HasActiveSource()
			currentGeneration := uint64(0)
			liveMP3 := false
			liveFormatReady := true
			liveMP3Matches := false
			if liveActive {
				currentGeneration = IcecastSource.GetSourceGeneration()
				liveMP3 = IcecastSource.IsMP3Source()
				if liveMP3 {
					liveMP3Matches, liveFormatReady = IcecastSource.MP3MatchesConfiguredOutput()
				}
			}

			if liveActive && liveFormatReady && (!strings.HasPrefix(mode, "live") || currentGeneration != liveGeneration) {
				stopSource()
				liveGeneration = currentGeneration
				engine.setLiveMode(true)
				engine.liveMP3Passthrough.Store(false)
				if liveMP3 && liveMP3Matches {
					mode = "live-mp3"
					engine.startLiveMP3Passthrough(liveGeneration)
				} else {
					mode = "live-transcode"
					var err error
					source, err = engine.startLiveSource(liveGeneration)
					if err != nil {
						Logger.Error(fmt.Sprintf("Failed to start live decoder: %v", err))
						retryAfter = now.Add(time.Second)
					}
					if liveMP3 {
						Logger.Info("Live MP3 format differs from configured output - FFmpeg transcoding enabled")
					}
				}
			}

			if !liveActive && strings.HasPrefix(mode, "live") {
				stopSource()
				engine.liveMP3Passthrough.Store(false)
				mode = "file"
				engine.setLiveMode(false)
				gapUntil = time.Time{}
				Logger.Info("Live source ended - returning to file PCM")
			}

			if mode == "" {
				mode = "file"
				engine.setLiveMode(false)
			}

			if source == nil && !now.Before(retryAfter) && !now.Before(gapUntil) {
				var err error
				if mode == "live-transcode" && liveActive {
					source, err = engine.startLiveSource(liveGeneration)
				} else if mode == "file" {
					source, err = engine.startFileSource()
				}
				if err != nil {
					Logger.Error(fmt.Sprintf("Failed to start %s source: %v", mode, err))
					retryAfter = now.Add(time.Second)
				}
			}

			pcm := make([]byte, bytesPerTick)
			finished := false
			if source != nil {
				wait := time.Duration(0)
				if mode == "live-transcode" {
					wait = liveUnderrunWait
				}
				pcm, finished = source.take(bytesPerTick, wait)
			}
			if _, err := engine.encoderInput.Write(pcm); err != nil {
				Logger.Error(fmt.Sprintf("Persistent encoder input failed: %v", err))
				return
			}

			if finished && source != nil {
				stopSource()
				if mode == "file" && Config.GapMs > 0 {
					gapUntil = now.Add(time.Duration(Config.GapMs) * time.Millisecond)
				}
			}
		}
	}
}

func (engine *ContinuousStreamEngine) publishEncoderOutput() {
	unitSize := engine.bitrateBps / 8 // approximately one second of MP3 data
	if unitSize < 4096 {
		unitSize = 4096
	}
	readBuffer := make([]byte, 16*1024)
	var pending []byte

	for {
		n, err := engine.encoderOut.Read(readBuffer)
		if n > 0 {
			pending = append(pending, readBuffer[:n]...)
			for len(pending) >= unitSize {
				unit := append([]byte(nil), pending[:unitSize]...)
				pending = pending[unitSize:]
				if !engine.liveMP3Passthrough.Load() {
					engine.publishUnit(unit, unitSize*8*1000/engine.bitrateBps)
				}
			}
		}
		if err != nil {
			select {
			case <-engine.stopCh:
				return
			default:
				Logger.Error(fmt.Sprintf("Persistent encoder output ended: %v", err))
				return
			}
		}
	}
}

func StartContinuousStream() error {
	engine, err := NewContinuousStreamEngine()
	if err != nil {
		return err
	}
	if err := engine.Start(); err != nil {
		return err
	}
	StreamEngine = engine
	return nil
}
