package geo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// GeoPreview holds the result of calling read_geo_data via the MCP server.
type GeoPreview struct {
	Kind string          `json:"kind"` // "geo_raster" or "geo_vector"
	Body string          `json:"body"` // full JSON from read_geo_data output
	URL  string          `json:"url"`  // preview URL (raster) or geojson URL (vector)
	Port int             `json:"port"` // HTTP server port
	Err  string          `json:"err,omitempty"`
}

var (
	previewClient   *MCPClient
	previewClientMu sync.Mutex
	previewClientDir string // project root
)

// SetPreviewProjectDir sets the working directory for the MCP server subprocess.
// Must be called once before GeneratePreview.
func SetPreviewProjectDir(dir string) {
	previewClientMu.Lock()
	defer previewClientMu.Unlock()
	previewClientDir = dir
}

// getMCPClient returns a cached MCP client, starting one if necessary.
func getMCPClient() (*MCPClient, error) {
	previewClientMu.Lock()
	defer previewClientMu.Unlock()

	if previewClient != nil {
		// Check if still alive
		return previewClient, nil
	}

	cwd := previewClientDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	python := findGeoPython()
	if python == "" {
		return nil, fmt.Errorf("geo preview: no Python interpreter configured (check geocode.json)")
	}

	var args []string
	args = append(args, python, "-m", "internal.geo.mcp_server")
	client, err := NewClient(cwd, args)
	if err != nil {
		return nil, err
	}
	previewClient = client
	return client, nil
}

// findGeoPython looks for the GEE conda environment Python.
func findGeoPython() string {
	candidates := []string{
		`D:\anaconda\anaconda3\envs\gee\python.exe`,
		`D:\Miniconda3\envs\gee\python.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// fallback: PATH
	return "python"
}

// GeneratePreview calls the geocode MCP server's read_geo_data tool for the
// given file and returns structured preview data (WebP URL for raster, GeoJSON
// URL for vector).
func GeneratePreview(projectDir, filePath string) (*GeoPreview, error) {
	SetPreviewProjectDir(projectDir)
	client, err := getMCPClient()
	if err != nil {
		return nil, err
	}

	result, err := client.CallTool("read_geo_data", map[string]any{
		"path": filePath,
	}, 0) // no timeout — let it complete
	if err != nil {
		return nil, fmt.Errorf("read_geo_data: %w", err)
	}

	// Parse result — MCP tools/call returns {content: [{type: "text", text: "..."}]}
	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &callResult); err != nil {
		return nil, fmt.Errorf("parse tools/call result: %w", err)
	}
	if len(callResult.Content) == 0 {
		return nil, fmt.Errorf("tools/call returned empty content")
	}

	// Parse the text as the geo preview JSON
	var geoData struct {
		GeoType        string `json:"__geo_type__"`
		PreviewURL     string `json:"preview_url"`
		GeoJSONURL     string `json:"geojson_url"`
		HTTPPort       int    `json:"http_port"`
		PreviewWidth   int    `json:"preview_width"`
		PreviewHeight  int    `json:"preview_height"`
		PreviewSize    int    `json:"preview_size_bytes"`
		RGBBands       []int  `json:"rgb_bands"`
		FeatureCount   int    `json:"feature_count"`
		Metadata       any    `json:"metadata"`
		Extent         any    `json:"extent"`
	}
	if err := json.Unmarshal([]byte(callResult.Content[0].Text), &geoData); err != nil {
		return nil, fmt.Errorf("parse geo preview JSON: %w", err)
	}

	preview := &GeoPreview{Port: geoData.HTTPPort}
	if geoData.GeoType == "raster_preview" {
		preview.Kind = "geo_raster"
		preview.URL = geoData.PreviewURL
	} else if geoData.GeoType == "vector_preview" {
		preview.Kind = "geo_vector"
		preview.URL = geoData.GeoJSONURL
	} else {
		return nil, fmt.Errorf("unknown geo type: %s", geoData.GeoType)
	}

	// Build the full body JSON matching what the frontend expects
	body := map[string]any{
		"__geo_type__":     geoData.GeoType,
		"preview_url":      geoData.PreviewURL,
		"geojson_url":      geoData.GeoJSONURL,
		"http_port":        geoData.HTTPPort,
		"preview_width":    geoData.PreviewWidth,
		"preview_height":   geoData.PreviewHeight,
		"preview_size_bytes": geoData.PreviewSize,
		"rgb_bands":        geoData.RGBBands,
		"feature_count":    geoData.FeatureCount,
		"metadata":         geoData.Metadata,
		"extent":           geoData.Extent,
	}
	bodyBytes, _ := json.Marshal(body)
	preview.Body = string(bodyBytes)
	return preview, nil
}

// ClosePreviewClient shuts down the cached MCP client if running.
func ClosePreviewClient() {
	previewClientMu.Lock()
	defer previewClientMu.Unlock()
	if previewClient != nil {
		previewClient.Close()
		previewClient = nil
	}
}

// ── Geo file detection (used by app.go ReadFile) ────────────────

// GeoPreviewKind returns the preview kind and basic metadata for a geo file.
// If the MCP server is available, generates a full preview. Otherwise falls
// back to basic metadata.
func GeoPreviewKind(projectDir, absPath string) (kind string, body string, url string) {
	return geoPreviewKind(projectDir, absPath)
}

func geoPreviewKind(projectDir, absPath string) (kind string, body string, url string) {
	ext := strings.ToLower(filepath.Ext(absPath))
	isGeo := ext == ".tif" || ext == ".tiff" || ext == ".shp" || ext == ".geojson"
	if !isGeo {
		return "", "", ""
	}

	// Try generating a full preview via MCP server
	preview, err := GeneratePreview(projectDir, absPath)
	if err == nil && preview != nil && preview.Body != "" {
		return preview.Kind, preview.Body, preview.URL
	}

	// Fallback: basic metadata without MCP
	if ext == ".tif" || ext == ".tiff" {
		return "geo_raster", geoRasterMetaJSON(absPath), ""
	}
	return "geo_vector", geoVectorMetaJSON(absPath), ""
}

func geoRasterMetaJSON(path string) string {
	b, _ := json.Marshal(map[string]any{
		"__geo_type__": "raster_preview",
		"preview_url":  nil,
		"metadata": map[string]any{
			"data_type": "raster",
			"path":      filepath.ToSlash(path),
			"driver":    "GTiff",
		},
	})
	return string(b)
}

func geoVectorMetaJSON(path string) string {
	b, _ := json.Marshal(map[string]any{
		"__geo_type__":  "vector_preview",
		"geojson_url":   nil,
		"feature_count": 0,
		"metadata": map[string]any{
			"data_type": "vector",
			"path":      filepath.ToSlash(path),
		},
	})
	return string(b)
}
