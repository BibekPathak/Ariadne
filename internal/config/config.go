package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string

	RequestyAPIKey string
	RequestyBase   string
	LLMModel       string
	EmbeddingModel string

	RedisAddr string

	MemoryEnabled   bool
	MemoryShortTTL  time.Duration
	MemoryVectorDir string

	WorkerMode string // "embedded" or "remote"
	NATSURL    string

	SandboxImage   string
	SandboxCPU     string
	SandboxMemMB   int
	SandboxNetwork string

	ArtifactsDir      string
	RepoBaseDir       string
	DemoRepoPath      string
	MaxIterations     int
	Timeout           time.Duration
	EngineConcurrency int
}

func Load() Config {
	return Config{
		DatabaseURL:    get("DATABASE_URL", "postgres://kubeai:kubeai@localhost:5432/kubeai?sslmode=disable"),
		HTTPAddr:       get("HTTP_ADDR", ":8080"),
		RequestyAPIKey: os.Getenv("REQUESTY_API_KEY"),
		RequestyBase:   get("REQUESTY_BASE_URL", "https://requesty.ai/v1"),
		LLMModel:       get("LLM_MODEL", "deepseek/deepseek-v4-flash"),
		EmbeddingModel: get("EMBEDDING_MODEL", "text-embedding-3-small"),

		RedisAddr: get("REDIS_ADDR", "localhost:6379"),

		MemoryEnabled:   getBool("MEMORY_ENABLED", true),
		MemoryShortTTL:  time.Duration(getInt("MEMORY_SHORT_TTL_MIN", 60)) * time.Minute,
		MemoryVectorDir: get("MEMORY_VECTOR_DIR", "./data/memory"),

		WorkerMode:        get("WORKER_MODE", "embedded"),
		NATSURL:           get("NATS_URL", "nats://localhost:4222"),
		SandboxImage:      get("SANDBOX_IMAGE", "kubeai-sandbox:local"),
		SandboxCPU:        get("SANDBOX_CPU", "1"),
		SandboxMemMB:      getInt("SANDBOX_MEM_MB", 1024),
		SandboxNetwork:    get("SANDBOX_NETWORK", "none"),
		ArtifactsDir:      get("ARTIFACTS_DIR", "./artifacts"),
		RepoBaseDir:       get("REPO_BASE_DIR", "/tmp/kubeai-repos"),
		DemoRepoPath:      get("DEMO_REPO_PATH", "./demo/repo"),
		MaxIterations:     getInt("MAX_ITERATIONS", 25),
		Timeout:           time.Duration(getInt("AGENT_TIMEOUT_MIN", 30)) * time.Minute,
		EngineConcurrency: getInt("ENGINE_CONCURRENCY", 4),
	}
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
