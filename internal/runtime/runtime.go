// Package runtime builds the execution stack shared by the control plane and
// standalone workers, so both binaries wire the same worker, tools, memory,
// sandbox and provider.
package runtime

import (
	"log/slog"
	"time"

	"adriane/internal/artifacts"
	"adriane/internal/checkpoint"
	"adriane/internal/config"
	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/memory"
	"adriane/internal/router"
	"adriane/internal/sandbox"
	"adriane/internal/store"
	"adriane/internal/tasks"
	"adriane/internal/tools"
	"adriane/internal/worker"
)

type Stack struct {
	Router        *router.Router
	Provider      llm.Provider // == Router (implements llm.Provider)
	TaskTemplates *tasks.Registry
	Worker        *worker.Worker
	Memory        memory.Memory
}

// Build constructs the worker stack. bus is the event publisher the worker
// emits through (in-memory in embedded mode, NATS in remote mode).
func Build(cfg config.Config, st *store.Store, bus events.Publisher, logger *slog.Logger) (*Stack, error) {
	arts, err := artifacts.New(cfg.ArtifactsDir, st.Artifacts)
	if err != nil {
		return nil, err
	}

	rtr := buildRouter(cfg, logger)
	toolRegistry := tools.NewRegistry(
		tools.ReadFileTool{}, tools.WriteFileTool{}, tools.ListFilesTool{},
		tools.ShellTool{}, tools.GitTool{}, tools.HTTPGetTool{},
	)
	taskTemplates := tasks.NewRegistry()
	sbox := sandboxFor(cfg, logger)

	mem, useSemantic := buildMemory(cfg, st, rtr, logger)

	var cps checkpoint.Store
	if cfg.CheckpointEnabled {
		cps = checkpoint.NewPostgres(st.Checkpoints)
	}

	w := worker.New(worker.Config{
		MaxIterations: cfg.MaxIterations,
		RepoBaseDir:   cfg.RepoBaseDir,
		Sandbox:       sbox,
		Memory:        mem,
		UseSemantic:   useSemantic,
		Checkpoints:   cps,
	}, rtr, toolRegistry, taskTemplates, ctxbuilder.New(), arts, bus, logger)

	return &Stack{Router: rtr, Provider: rtr, TaskTemplates: taskTemplates, Worker: w, Memory: mem}, nil
}

func buildRouter(cfg config.Config, logger *slog.Logger) *router.Router {
	if cfg.RouterPrimaryKey == "" {
		logger.Warn("no ROUTER_PRIMARY_API_KEY set; using demo provider (offline mode)")
	}
	rtr := router.New(router.Config{
		FastModel:      cfg.RouterFastModel,
		CodingModel:    cfg.RouterCodingModel,
		ReasoningModel: cfg.RouterReasoningModel,
		VisionModel:    cfg.RouterVisionModel,
		PrimaryURL:     cfg.RouterPrimaryURL,
		PrimaryKey:     cfg.RouterPrimaryKey,
		FallbackURL:    cfg.RouterFallbackURL,
		FallbackKey:    cfg.RouterFallbackKey,
		EmbeddingModel: cfg.EmbeddingModel,
		DefaultPolicy:  router.PlannerPolicy(),
	})
	if cfg.RouterFallbackKey != "" {
		logger.Info("model router", "fallback", cfg.RouterFallbackURL)
	}
	return rtr
}

func sandboxFor(cfg config.Config, logger *slog.Logger) sandbox.Sandbox {
	switch cfg.SandboxRuntime {
	case "firecracker":
		sb := sandbox.NewFirecrackerSandbox(sandbox.FirecrackerConfig{
			Binary:      cfg.FirecrackerBinary,
			Kernel:      cfg.FirecrackerKernel,
			RootFS:      cfg.FirecrackerRootFS,
			WorkDir:     cfg.FirecrackerWorkDir,
			VCPU:        cfg.FirecrackerVCPU,
			MemMiB:      cfg.FirecrackerMemMiB,
			Port:        cfg.FirecrackerPort,
			PoolSize:    cfg.FirecrackerPool,
			BootTimeout: 90 * time.Second,
		}, logger)
		logger.Info("sandbox runtime: firecracker", "kernel", cfg.FirecrackerKernel, "rootfs", cfg.FirecrackerRootFS)
		return sb
	default:
		return sandbox.NewDockerSandbox(sandbox.DockerConfig{
			Image:   cfg.SandboxImage,
			CPU:     cfg.SandboxCPU,
			MemMB:   cfg.SandboxMemMB,
			Network: cfg.SandboxNetwork,
			BaseDir: cfg.RepoBaseDir,

			ReadOnlyRoot: cfg.SandboxReadOnly,
			CapDropAll:   cfg.SandboxCapDrop,
			PidsLimit:    cfg.SandboxPidsLimit,
			RunAsUser:    cfg.SandboxUser,
		})
	}
}

func buildMemory(cfg config.Config, st *store.Store, provider llm.Provider, logger *slog.Logger) (memory.Memory, bool) {
	if !cfg.MemoryEnabled {
		return nil, false
	}
	short, err := memory.NewShortTermRedis(cfg.RedisAddr, cfg.MemoryShortTTL)
	if err != nil {
		logger.Warn("memory short-term disabled", "err", err)
		return nil, false
	}
	vec, err := memory.NewVectorStore(cfg.MemoryVectorDir)
	if err != nil {
		logger.Warn("memory semantic disabled", "err", err)
		_ = short.Close()
		return nil, false
	}
	m, err := memory.NewManager(memory.ManagerConfig{
		ShortTerm: short,
		LongTerm:  memory.NewLongTermPostgres(st.Memories),
		Semantic:  vec,
		Embedder:  provider,
		Logger:    logger,
	})
	if err != nil {
		logger.Warn("memory manager disabled", "err", err)
		_ = short.Close()
		return nil, false
	}
	return m, true
}
