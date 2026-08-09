<?php

/**
 * GoStream API Client
 * 
 * PHP class for controlling the GoStream server via HTTP API
 * Supports all public and authenticated endpoints
 * 
 * @version 1.0
 * @author GoStream
 */
class GoStreamClient {
    
    private $baseUrl;
    private $username;
    private $password;
    private $timeout;
    private $lastError;
    private $lastResponse;
    
    /**
     * Initialize GoStream Client
     * 
     * @param string $baseUrl Server base URL (e.g., http://localhost:8080)
     * @param string $username Username for authentication (optional)
     * @param string $password Password for authentication (optional)
     * @param int $timeout Request timeout in seconds (default: 30)
     */
    public function __construct($baseUrl, $username = null, $password = null, $timeout = 30) {
        $this->baseUrl = rtrim($baseUrl, '/');
        $this->username = $username;
        $this->password = $password;
        $this->timeout = $timeout;
        $this->lastError = null;
        $this->lastResponse = null;
    }
    
    /**
     * Get last error message
     * @return string|null
     */
    public function getLastError() {
        return $this->lastError;
    }
    
    /**
     * Get last full response
     * @return array|null
     */
    public function getLastResponse() {
        return $this->lastResponse;
    }
    
    /**
     * Make HTTP request to API
     * 
     * @param string $method HTTP method (GET, POST, DELETE)
     * @param string $endpoint API endpoint path
     * @param array $params Query parameters
     * @param bool $requiresAuth Whether endpoint requires authentication
     * @return array|false Response data or false on error
     */
    private function request($method, $endpoint, $params = [], $requiresAuth = false) {
        $url = $this->baseUrl . $endpoint;
        
        // Add query parameters (all methods use query strings for this API)
        if (!empty($params)) {
            $url .= '?' . http_build_query($params);
        }
        
        // Initialize cURL
        $ch = curl_init();
        curl_setopt($ch, CURLOPT_URL, $url);
        curl_setopt($ch, CURLOPT_TIMEOUT, $this->timeout);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_CUSTOMREQUEST, $method);
        
        // Set method-specific options
        if ($method === 'POST' || $method === 'DELETE') {
            curl_setopt($ch, CURLOPT_CUSTOMREQUEST, $method);
        }
        
        // Add authentication header if required
        if ($requiresAuth && $this->username && $this->password) {
            $auth = base64_encode($this->username . ':' . $this->password);
            curl_setopt($ch, CURLOPT_HTTPHEADER, [
                'Authorization: Basic ' . $auth,
                'Content-Type: application/json'
            ]);
        }
        
        // Execute request
        $response = curl_exec($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $curlError = curl_error($ch);
        curl_close($ch);
        
        // Handle cURL errors
        if ($response === false) {
            $this->lastError = "cURL Error: " . $curlError;
            $this->lastResponse = null;
            return false;
        }
        
        // Handle HTTP errors
        if ($httpCode >= 400) {
            $this->lastError = "HTTP Error: " . $httpCode;
            $this->lastResponse = null;
            return false;
        }
        
        // Parse JSON response
        $data = json_decode($response, true);
        if (json_last_error() !== JSON_ERROR_NONE) {
            $this->lastError = "Invalid JSON response: " . json_last_error_msg();
            $this->lastResponse = null;
            return false;
        }
        
        $this->lastError = null;
        $this->lastResponse = $data;
        return $data;
    }
    
    // ==================== PUBLIC ENDPOINTS ====================
    
    /**
     * Get the audio stream (raw MP3 data)
     * 
     * @param string $savePath Optional file path to save the stream
     * @return resource|false Stream resource or false on error
     */
    public function getStream($savePath = null) {
        $ch = curl_init($this->baseUrl . '/');
        curl_setopt($ch, CURLOPT_TIMEOUT, 0); // No timeout for streaming
        curl_setopt($ch, CURLOPT_BINARYTRANSFER, true);
        
        if ($savePath) {
            $fp = fopen($savePath, 'w');
            curl_setopt($ch, CURLOPT_FILE, $fp);
        } else {
            curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        }
        
        $result = curl_exec($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);
        
        if ($savePath) {
            fclose($fp);
        }
        
        if ($httpCode >= 400) {
            $this->lastError = "Failed to get stream: HTTP " . $httpCode;
            return false;
        }
        
        return $result;
    }
    
    /**
     * Get server information
     * 
     * @return array|false Server info with current music details
     */
    public function getInfo() {
        return $this->request('GET', '/info');
    }
    
    /**
     * Get Icecast-compatible stats
     * 
     * @return array|false Stream statistics
     */
    public function getStats() {
        return $this->request('GET', '/stats');
    }
    
    /**
     * Get current stream status
     * 
     * @return array|false Stream status with now playing info
     */
    public function getStatus() {
        return $this->request('GET', '/status');
    }
    
    /**
     * Get next song information
     * 
     * @return array|false Next song details
     */
    public function getNextSong() {
        return $this->request('GET', '/next');
    }
    
    /**
     * Get list of all available songs
     * 
     * @return array|false List of songs with hash IDs
     */
    public function getSongs() {
        return $this->request('GET', '/songs');
    }
    
    /**
     * Get system and stream metrics
     * 
     * @return array|false Metrics including listeners, bandwidth, memory, uptime
     */
    public function getMetrics() {
        return $this->request('GET', '/metrics');
    }
    
    /**
     * Get current streaming mode
     * 
     * @return array|false Mode info (file or icecast)
     */
    public function getMode() {
        return $this->request('GET', '/mode');
    }
    
    // ==================== PROTECTED ENDPOINTS ====================
    
    /**
     * Skip to next song
     * Requires authentication
     * 
     * @return array|false Status and next song info
     */
    public function skipSong() {
        return $this->request('GET', '/skip', [], true);
    }
    
    /**
     * Set next song by hash
     * Requires authentication
     * 
     * @param string $songHash Hash ID of the song
     * @return array|false Status and song info
     */
    public function setNextSong($songHash) {
        return $this->request('POST', '/next/set', ['hash' => $songHash], true);
    }
    
    /**
     * Add song to playlist
     * Requires authentication
     * 
     * @param string $songHash Hash ID of the song
     * @return array|false Status and song info
     */
    public function addToPlaylist($songHash) {
        return $this->request('POST', '/playlist/add', ['hash' => $songHash], true);
    }
    
    /**
     * Remove song from playlist
     * Requires authentication
     * 
     * @param int $index Position in playlist (0-indexed)
     * @return array|false Status
     */
    public function removeFromPlaylist($index) {
        return $this->request('DELETE', '/playlist/remove', ['index' => $index], true);
    }
    
    /**
     * Get current playlist
     * Requires authentication
     * 
     * @return array|false Current playlist
     */
    public function getPlaylist() {
        return $this->request('GET', '/playlist', [], true);
    }
    
    /**
     * Clear all songs from playlist
     * Requires authentication
     * 
     * @return array|false Status
     */
    public function clearPlaylist() {
        return $this->request('DELETE', '/playlist', [], true);
    }
    
    /**
     * Reorder playlist
     * Requires authentication
     * 
     * @param int $from Current index
     * @param int $to New index
     * @return array|false Status
     */
    public function reorderPlaylist($from, $to) {
        return $this->request('POST', '/playlist/reorder', [
            'from' => $from,
            'to' => $to
        ], true);
    }
    
    /**
     * Enable Icecast live streaming mode
     * Requires authentication
     * 
     * @return array|false Status
     */
    public function enableIcecastMode() {
        return $this->request('POST', '/icecast/enable', [], true);
    }
    
    /**
     * Disable Icecast mode and revert to file streaming
     * Requires authentication
     * 
     * @return array|false Status
     */
    public function disableIcecastMode() {
        return $this->request('POST', '/icecast/disable', [], true);
    }
    
    // ==================== HELPER METHODS ====================
    
    /**
     * Find a song by name and return its hash
     * 
     * @param string $songName Part of song name or filename to search for
     * @return string|null Hash of the first matching song
     */
    public function findSongByName($songName) {
        $songs = $this->getSongs();
        if (!$songs || !isset($songs['songs'])) {
            return null;
        }
        
        $searchLower = strtolower($songName);
        foreach ($songs['songs'] as $song) {
            if (strpos(strtolower($song['title']), $searchLower) !== false || 
                strpos(strtolower($song['filename']), $searchLower) !== false) {
                return $song['hash'];
            }
        }
        
        return null;
    }
    
    /**
     * Get now playing song title
     * 
     * @return string|null Currently playing song title
     */
    public function getNowPlaying() {
        $status = $this->getStatus();
        if (!$status || !isset($status['now_playing']['title'])) {
            return null;
        }
        return $status['now_playing']['title'];
    }
    
    /**
     * Get active listener count
     * 
     * @return int|null Number of active listeners
     */
    public function getListenerCount() {
        $metrics = $this->getMetrics();
        if (!$metrics || !isset($metrics['metrics']['active_listeners'])) {
            return null;
        }
        return $metrics['metrics']['active_listeners'];
    }
    
    /**
     * Get stream uptime formatted
     * 
     * @return string|null Formatted uptime (HH:MM:SS)
     */
    public function getUptime() {
        $metrics = $this->getMetrics();
        if (!$metrics || !isset($metrics['metrics']['stream_uptime']['formatted'])) {
            return null;
        }
        return $metrics['metrics']['stream_uptime']['formatted'];
    }
    
    /**
     * Get total data streamed (human readable)
     * 
     * @return string|null Total data streamed in human readable format
     */
    public function getTotalDataStreamed() {
        $metrics = $this->getMetrics();
        if (!$metrics || !isset($metrics['metrics']['total_data_streamed']['human'])) {
            return null;
        }
        return $metrics['metrics']['total_data_streamed']['human'];
    }
    
    /**
     * Get current bandwidth in Mbps
     * 
     * @return float|null Current bandwidth
     */
    public function getBandwidth() {
        $metrics = $this->getMetrics();
        if (!$metrics || !isset($metrics['metrics']['bandwidth']['raw_mbps'])) {
            return null;
        }
        return $metrics['metrics']['bandwidth']['raw_mbps'];
    }
}
