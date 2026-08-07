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

	CheckpointEnabled bool

	SandboxReadOnly  bool
	SandboxCapDrop   bool
	SandboxPidsLimit int
	SandboxUser      string

	SandboxRuntime string // docker | firecracker

	FirecrackerBinary  string
	FirecrackerKernel  string
	FirecrackerRootFS  string
	FirecrackerWorkDir string
	FirecrackerPool    int
	FirecrackerPort    int
	FirecrackerVCPU    int
	FirecrackerMemMiB  int

	RouterFastModel      string
	RouterCodingModel    string
	RouterReasoningModel string
	RouterVisionModel    string
	RouterPrimaryURL     string
	RouterPrimaryKey     string
	RouterFallbackURL    string
	RouterFallbackKey    string

	// Auth (Phase 10)
	AuthEnabled bool
	AdminAPIKey string

	// Quotas (Phase 10)
	OrgMaxConcurrentAgents int
	OrgMaxDailyAgents      int
	OrgDailyCostCap        float64

	// Rate limiting (Phase 10)
	RateLimitPerMin int

	// Leadership (Phase 10)
	LeaderEnabled bool

	// Autoscaling (Phase 10)
	WorkerAutoscale  bool
	WorkerMin        int
	WorkerMax        int
	ScaleUpThreshold int
	ScaleDownIdle    time.Duration
	WorkerBinary     string
	AutoscalerPoll   time.Duration

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

		WorkerMode: get("WORKER_MODE", "embedded"),
		NATSURL:    get("NATS_URL", "nats://localhost:4222"),

		CheckpointEnabled: getBool("CHECKPOINT_ENABLED", true),

		SandboxReadOnly:  getBool("SANDBOX_READ_ONLY", true),
		SandboxCapDrop:   getBool("SANDBOX_CAP_DROP", true),
		SandboxPidsLimit: getInt("SANDBOX_PIDS_LIMIT", 256),
		SandboxUser:      get("SANDBOX_USER", "1000:1000"),

		SandboxRuntime: get("SANDBOX_RUNTIME", "docker"),

		FirecrackerBinary:  get("FIRECRACKER_BINARY", "./deploy/firecracker/firecracker"),
		FirecrackerKernel:  get("FIRECRACKER_KERNEL", "./deploy/firecracker/vmlinux.bin"),
		FirecrackerRootFS:  get("FIRECRACKER_ROOTFS", "./deploy/firecracker/rootfs.ext4"),
		FirecrackerWorkDir: get("FIRECRACKER_WORKDIR", "./data/firecracker"),
		FirecrackerPool:    getInt("FIRECRACKER_POOL", 1),
		FirecrackerPort:    getInt("FIRECRACKER_PORT", 5200),
		FirecrackerVCPU:    getInt("FIRECRACKER_VCPU", 2),
		FirecrackerMemMiB:  getInt("FIRECRACKER_MEM_MIB", 1024),

		RouterFastModel:      get("ROUTER_FAST_MODEL", ""),
		RouterCodingModel:    get("ROUTER_CODING_MODEL", os.Getenv("LLM_MODEL")),
		RouterReasoningModel: get("ROUTER_REASONING_MODEL", ""),
		RouterVisionModel:    get("ROUTER_VISION_MODEL", ""),
		RouterPrimaryURL:     get("ROUTER_PRIMARY_URL", get("REQUESTY_BASE_URL", "https://requesty.ai/v1")),
		RouterPrimaryKey:     get("ROUTER_PRIMARY_API_KEY", os.Getenv("REQUESTY_API_KEY")),
		RouterFallbackURL:    get("ROUTER_FALLBACK_URL", ""),
		RouterFallbackKey:    get("ROUTER_FALLBACK_API_KEY", ""),

		AuthEnabled: getBool("AUTH_ENABLED", true),
		AdminAPIKey: get("ADMIN_API_KEY", "adr-dev-admin"),

		OrgMaxConcurrentAgents: getInt("ORG_MAX_CONCURRENT_AGENTS", 3),
		OrgMaxDailyAgents:      getInt("ORG_MAX_DAILY_AGENTS", 100),
		OrgDailyCostCap:        getFloat("ORG_DAILY_COST_CAP", 1.0),

		RateLimitPerMin: getInt("RATE_LIMIT_PER_MIN", 10),

		LeaderEnabled: getBool("LEADER_ENABLED", true),

		WorkerAutoscale:   getBool("WORKER_AUTOSCALE", false),
		WorkerMin:         getInt("WORKER_MIN", 1),
		WorkerMax:         getInt("WORKER_MAX", 8),
		ScaleUpThreshold:  getInt("SCALE_UP_THRESHOLD", 10),
		ScaleDownIdle:     time.Duration(getInt("SCALE_DOWN_IDLE_SEC", 60)) * time.Second,
		WorkerBinary:      get("WORKER_BINARY", "./bin/kubeai-worker"),
		AutoscalerPoll:    time.Duration(getInt("AUTOSCALE_POLL_MS", 1000)) * time.Millisecond,
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

func getFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
