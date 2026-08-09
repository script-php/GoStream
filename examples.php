<?php

/**
 * GoStream API Client - Usage Examples
 * 
 * Demonstrates how to use the GoStreamClient class
 */

require_once 'GoStreamClient.php';

// Initialize the client
$client = new GoStreamClient(
    'http://localhost:8080',        // Server URL
    'admin',                         // Username (for authenticated endpoints)
    'password'                       // Password (for authenticated endpoints)
);

// ==================== PUBLIC ENDPOINTS EXAMPLES ====================

echo "=== PUBLIC ENDPOINTS ===\n\n";

// Get server info
$info = $client->getInfo();
if ($info) {
    echo "Server Name: " . $info['Name'] . "\n";
    echo "Version: " . $info['Version'] . "\n";
} else {
    echo "Error: " . $client->getLastError() . "\n";
}

// Get current status
$status = $client->getStatus();
if ($status) {
    echo "Now Playing: " . $status['now_playing']['title'] . "\n";
    echo "Artist: " . $status['now_playing']['artist'] . "\n";
    echo "Bitrate: " . $status['now_playing']['bitrate'] . "\n";
}

// Get metrics
$metrics = $client->getMetrics();
if ($metrics) {
    echo "Active Listeners: " . $metrics['metrics']['active_listeners'] . "\n";
    echo "Stream Uptime: " . $metrics['metrics']['stream_uptime']['formatted'] . "\n";
    echo "Data Streamed: " . $metrics['metrics']['total_data_streamed']['human'] . "\n";
    echo "Current Bandwidth: " . $metrics['metrics']['bandwidth']['current_mbps'] . "\n";
}

// Get all songs
$songs = $client->getSongs();
if ($songs) {
    echo "Total Songs: " . $songs['total'] . "\n";
    foreach ($songs['songs'] as $song) {
        echo "  - " . $song['title'] . " by " . $song['artist'] . " [" . $song['hash'] . "]\n";
    }
}

// Get streaming mode
$mode = $client->getMode();
if ($mode) {
    echo "Current Mode: " . $mode['mode'] . "\n";
}

// ==================== PROTECTED ENDPOINTS EXAMPLES ====================

echo "\n=== PROTECTED ENDPOINTS ===\n\n";

// Skip to next song
$skip = $client->skipSong();
if ($skip) {
    echo "Skipped! Now playing: " . $skip['now_playing']['title'] . "\n";
} else {
    echo "Error: " . $client->getLastError() . "\n";
}

// Set next song by hash
$setNext = $client->setNextSong('song-hash-here');
if ($setNext) {
    echo "Next song set to: " . $setNext['next_song']['title'] . "\n";
}

// Add song to playlist
$add = $client->addToPlaylist('song-hash-here');
if ($add) {
    echo "Added to playlist: " . $add['song']['title'] . "\n";
}

// Get current playlist
$playlist = $client->getPlaylist();
if ($playlist) {
    echo "Playlist (" . $playlist['total'] . " songs):\n";
    foreach ($playlist['playlist'] as $item) {
        echo "  " . ($item['index'] + 1) . ". " . $item['title'] . " by " . $item['artist'] . "\n";
    }
}

// Remove song from playlist (index 0)
$remove = $client->removeFromPlaylist(0);
if ($remove) {
    echo "Song removed from playlist\n";
}

// Reorder playlist (move song from position 1 to position 0)
$reorder = $client->reorderPlaylist(1, 0);
if ($reorder) {
    echo "Playlist reordered\n";
}

// Clear entire playlist
$clear = $client->clearPlaylist();
if ($clear) {
    echo "Playlist cleared\n";
}

// ==================== HELPER METHODS EXAMPLES ====================

echo "\n=== HELPER METHODS ===\n\n";

// Find a song by name
$songHash = $client->findSongByName('Beatles');
if ($songHash) {
    echo "Found song: $songHash\n";
}

// Get now playing
$nowPlaying = $client->getNowPlaying();
echo "Now Playing: $nowPlaying\n";

// Get listener count
$listeners = $client->getListenerCount();
echo "Active Listeners: $listeners\n";

// Get uptime
$uptime = $client->getUptime();
echo "Stream Uptime: $uptime\n";

// Get total data streamed
$dataStreamed = $client->getTotalDataStreamed();
echo "Data Streamed: $dataStreamed\n";

// Get bandwidth
$bandwidth = $client->getBandwidth();
echo "Current Bandwidth: {$bandwidth} Mbps\n";

?>
