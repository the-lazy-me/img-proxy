// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/middleware/stdlib"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

var CONFIG struct {
	CustomDomain    string
	PathPrefix      string
	AllowedOrigins  []string
	APIKey          string
	StoragePath     string // 本地存储路径
	ListenAddr      string
	FileExpiryHours time.Duration // 文件过期时间（小时）
}

func init() {
	godotenv.Load()

	CONFIG.CustomDomain = getEnv("CUSTOM_DOMAIN", "https://images.qhaigc.net")
	CONFIG.PathPrefix = strings.TrimSuffix(getEnv("PATH_PREFIX", "img"), "/") + "/"
	CONFIG.AllowedOrigins = strings.Split(getEnv("ALLOWED_ORIGINS", "*"), ",")
	for i := range CONFIG.AllowedOrigins {
		CONFIG.AllowedOrigins[i] = strings.TrimSpace(CONFIG.AllowedOrigins[i])
	}
	CONFIG.APIKey = getEnv("API_KEY", "")
	CONFIG.StoragePath = getEnv("STORAGE_PATH", "./storage") // 容器内路径
	CONFIG.ListenAddr = getEnv("LISTEN_ADDR", ":8080")

	// 从环境变量读取过期时间，单位：小时
	expiryHours := getFloat64Env("FILE_EXPIRY_HOURS", 24.0)
	CONFIG.FileExpiryHours = time.Duration(expiryHours * float64(time.Hour))

	// 确保存储目录存在
	if err := os.MkdirAll(CONFIG.StoragePath, 0755); err != nil {
		log.Fatal("无法创建存储目录:", err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getFloat64Env(key string, fallback float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return fallback
}

func main() {
	// 🔁 启动后台清理任务（每隔 1 小时检查一次）
	go startCleanupTask()

	// 限流：100 次/分钟
	rate := limiter.Rate{
		Period: 1 * time.Minute,
		Limit:  100,
	}
	store := memory.NewStore()
	limiterMiddleware := stdlib.NewMiddleware(limiter.New(store, rate))

	http.HandleFunc("/", limiterMiddleware.Handler(http.HandlerFunc(rateLimitHandler)).ServeHTTP)
	log.Printf("服务器启动在 %s，存储路径: %s，文件过期时间: %.1f 小时", CONFIG.ListenAddr, CONFIG.StoragePath, CONFIG.FileExpiryHours.Hours())
	log.Fatal(http.ListenAndServe(CONFIG.ListenAddr, nil))
}

func rateLimitHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "https://api.qhaigc.net", http.StatusFound)
		return
	}

	origin := r.Header.Get("Origin")
	corsOrigin := "*"
	for _, o := range CONFIG.AllowedOrigins {
		if o == origin {
			corsOrigin = origin
			break
		}
	}

	corsHeaders := map[string]string{
		"Access-Control-Allow-Origin":  corsOrigin,
		"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type, Authorization, X-API-Key",
		"Access-Control-Max-Age":       "86400",
		"Content-Type":                 "application/json",
	}

	if r.Method == "OPTIONS" {
		for k, v := range corsHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.URL.Path == "/proxy" {
		handleProxy(w, r, corsHeaders)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/"+CONFIG.PathPrefix) {
		handleImage(w, r)
		return
	}

	http.Redirect(w, r, "https://api.qhaigc.net", http.StatusFound)
}

func handleProxy(w http.ResponseWriter, r *http.Request, corsHeaders map[string]string) {
	if r.Method != "POST" {
		responseError(w, corsHeaders, "请求失败")
		return
	}

	// API Key 验证
	if CONFIG.APIKey != "" && r.Header.Get("X-API-Key") != CONFIG.APIKey {
		log.Println("Invalid API key:", r.Header.Get("X-API-Key"))
		responseError(w, corsHeaders, "请求失败")
		return
	}

	// 解析 JSON
	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		responseError(w, corsHeaders, "请求失败")
		return
	}
	imageURL := strings.TrimSpace(data["url"])
	if imageURL == "" {
		responseError(w, corsHeaders, "请求失败")
		return
	}

	// 下载图片（最多重试 3 次）
	var imageData []byte
	var contentType string
	var err error

	for i := 0; i < 3; i++ {
		imageData, contentType, err = downloadImage(imageURL)
		if err == nil {
			break
		}
		log.Printf("[Retry %d/3] 下载失败: %v", i+1, err)
		if i < 2 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	if err != nil {
		log.Printf("重试失败: %v", err)
		responseError(w, corsHeaders, "图片上传失败，请联系管理员")
		return
	}

	// 文件扩展名
	ext := getExtFromContentType(contentType)
	if ext == "" {
		ext = "png"
	}

	// 生成文件名
	filename := fmt.Sprintf("%s%d-%s.%s", CONFIG.PathPrefix, time.Now().UnixNano(), randomString(6), ext)
	fullPath := filepath.Join(CONFIG.StoragePath, filename)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		log.Printf("创建目录失败: %v", err)
		responseError(w, corsHeaders, "存储失败")
		return
	}

	// 保存到本地
	if err := os.WriteFile(fullPath, imageData, 0644); err != nil {
		log.Printf("保存文件失败: %v", err)
		responseError(w, corsHeaders, "存储失败")
		return
	}

	// 返回结果
	proxyURL := fmt.Sprintf("%s/%s", CONFIG.CustomDomain, filename)
	result := map[string]interface{}{
		"success": true,
		"url":     proxyURL,
		"type":    contentType,
		"size":    len(imageData),
	}

	for k, v := range corsHeaders {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func downloadImage(imageURL string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 100))
		return nil, "", fmt.Errorf("server error: %d - %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 100))
		return nil, "", fmt.Errorf("client error: %d - %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	ct := resp.Header.Get("Content-Type")
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = ct[:idx]
	}
	ct = strings.TrimSpace(ct)

	return data, ct, nil
}

func handleImage(w http.ResponseWriter, r *http.Request) {
	// 构造本地文件路径
	filePath := filepath.Join(CONFIG.StoragePath, strings.TrimPrefix(r.URL.Path, "/"))

	// 检查文件是否存在
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) || info.IsDir() {
		http.Error(w, "图片不存在", http.StatusNotFound)
		return
	}

	// 推断 Content-Type
	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".gif":
		contentType = "image/gif"
	}

	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "读取失败", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", fmt.Sprintf(`"%x"`, info.ModTime().Unix()))

	io.Copy(w, f)
}

func responseError(w http.ResponseWriter, headers map[string]string, message string) {
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func getExtFromContentType(ct string) string {
	switch ct {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return "png"
	}
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}

// startCleanupTask 启动一个后台任务，定期清理过期文件
func startCleanupTask() {
	// 每隔 1 小时执行一次清理
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// 立即执行一次清理
	cleanupOldFiles()

	for {
		select {
		case <-ticker.C:
			cleanupOldFiles()
		}
	}
}

// cleanupOldFiles 删除超过 CONFIG.FileExpiryHours 的文件
func cleanupOldFiles() {
	now := time.Now()
	expiryDuration := CONFIG.FileExpiryHours

	err := filepath.Walk(CONFIG.StoragePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("访问文件出错 %s: %v", path, err)
			return nil // 继续遍历其他文件
		}

		// 只处理文件，跳过目录
		if info.IsDir() {
			return nil
		}

		// 计算文件年龄
		age := now.Sub(info.ModTime())
		if age > expiryDuration {
			// 删除过期文件
			if err := os.Remove(path); err != nil {
				log.Printf("删除过期文件失败 %s: %v", path, err)
			} else {
				log.Printf("已删除过期文件: %s (已存在 %.1f 小时)", path, age.Hours())
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("清理任务执行失败: %v", err)
	}
}
