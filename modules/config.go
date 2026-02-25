package modules

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gostream/conf"
)

type IConfig struct {
	Port               int
	Host               string
	Directory          string
	Random             bool
	Debug              bool
	Name               string
	Time               int64
	Version            string
	GapMs              int    // Gap/silence between songs in milliseconds
	Normalize          bool   // Normalize all audio to same bitrate/samplerate using ffmpeg
	StandardBitrate    string // Bitrate for audio normalization (e.g., "128k")
	StandardSampleRate string // Sample rate for audio normalization (e.g., "44100")
	CacheDir           string // Directory to store cached normalized files
	CacheTTLMinutes    int    // Cache time-to-live in minutes (0 = no cleanup)
}

var Config *IConfig

// JSONConfig represents the structure of a config JSON file
type JSONConfig struct {
	Port               int    `json:"port"`
	Host               string `json:"host"`
	Directory          string `json:"directory"`
	Random             bool   `json:"random"`
	Debug              bool   `json:"debug"`
	Name               string `json:"name"`
	GapMs              int    `json:"gap_ms"`
	Normalize          bool   `json:"normalize"`
	StandardBitrate    string `json:"standard_bitrate"`
	StandardSampleRate string `json:"standard_sample_rate"`
	CacheDir           string `json:"cache_dir"`
	CacheTTLMinutes    int    `json:"cache_ttl_minutes"`
}

// LoadConfigFromFile loads configuration from a local JSON file
func LoadConfigFromFile(filepath string) (*JSONConfig, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config JSONConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// LoadConfigFromURL loads configuration from a remote JSON URL
func LoadConfigFromURL(url string) (*JSONConfig, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch config from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch config, status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var config JSONConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	return &config, nil
}

// LoadConfig loads configuration from file or URL
func LoadConfig(source string) (*JSONConfig, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return LoadConfigFromURL(source)
	}
	return LoadConfigFromFile(source)
}

func init() {

	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	var port int
	var host string
	var random bool
	var directory string
	var debug bool
	var help bool
	var name string
	var gap int
	var normalize bool
	var configSource string
	var standardBitrate string = "128k"
	var standardSampleRate string = "44100"
	var cacheDir string = ".cache"
	var cacheTTLMinutes int = 10

	flag.StringVar(&name, "n", "GoStream", "server name")
	flag.IntVar(&port, "p", 8090, "server port number")
	flag.StringVar(&host, "host", "0.0.0.0", "server host address")
	flag.BoolVar(&random, "r", false, "enable random playback mode")
	flag.BoolVar(&debug, "debug", false, "enable debug mode for server")
	flag.StringVar(&directory, "d", root, "directory to play")
	flag.IntVar(&gap, "gap", 500, "gap/silence between songs in milliseconds")
	flag.BoolVar(&normalize, "normalize", false, "normalize audio to standard bitrate/samplerate using ffmpeg")
	flag.StringVar(&configSource, "c", "", "config file or URL (e.g., config.json or https://example.com/config.json)")
	flag.BoolVar(&help, "h", false, "show help information")

	flag.Parse()

	if help {
		fmt.Println("Usage: GoStream [options]")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Load config from JSON if provided
	if configSource != "" {
		jsonConfig, err := LoadConfig(configSource)
		if err != nil {
			log.Fatal("Error loading config:", err)
		}

		// Apply JSON config values (only if not already set by defaults)
		// Check if flags were explicitly provided by comparing with defaults
		if jsonConfig.Port != 0 && port == 8090 {
			port = jsonConfig.Port
		}
		if jsonConfig.Host != "" && host == "0.0.0.0" {
			host = jsonConfig.Host
		}
		if jsonConfig.Directory != "" {
			directory = jsonConfig.Directory
		}
		if jsonConfig.Name != "" && name == "GoStream" {
			name = jsonConfig.Name
		}
		if jsonConfig.GapMs != 0 && gap == 500 {
			gap = jsonConfig.GapMs
		}
		if jsonConfig.StandardBitrate != "" {
			standardBitrate = jsonConfig.StandardBitrate
		}
		if jsonConfig.StandardSampleRate != "" {
			standardSampleRate = jsonConfig.StandardSampleRate
		}
		if jsonConfig.CacheDir != "" {
			cacheDir = jsonConfig.CacheDir
		}
		if jsonConfig.CacheTTLMinutes != 0 {
			cacheTTLMinutes = jsonConfig.CacheTTLMinutes
		}
		
		// Boolean flags - only override if they're true in config
		if jsonConfig.Random {
			random = true
		}
		if jsonConfig.Debug {
			debug = true
		}
		if jsonConfig.Normalize {
			normalize = true
		}
	}

	directory, err = filepath.Abs(directory)

	if err != nil {
		log.Fatal(err)
	}

	Config = &IConfig{
		Port:               port,
		Host:               host,
		Random:             random,
		Directory:          directory,
		Debug:              debug,
		Time:               time.Now().UnixNano() / int64(time.Millisecond),
		Name:               name,
		Version:            conf.CodeVersion,
		GapMs:              gap,
		Normalize:          normalize,
		CacheTTLMinutes:    cacheTTLMinutes,
		StandardBitrate:    standardBitrate,
		StandardSampleRate: standardSampleRate,
		CacheDir:           cacheDir,
	}
}

func GetConfig() *IConfig {
	return Config
}
