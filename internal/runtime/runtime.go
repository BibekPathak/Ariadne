// Package runtime builds the execution stack shared by the control plane and
// standalone workers, so both binaries wire the same worker, tools, memory,
// sandbox and provider.
package runtime

import (
	"log/slog"

	"adriane/internal/artifacts"
	"adriane/internal/checkpoint"
	"adriane/internal/config"
	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/memory"
	"adriane/internal/sandbox"
	"adriane/internal/store"
	"adriane/internal/tasks"
	"adriane/internal/tools"
	"adriane/internal/worker"
)

type Stack struct {
	Provider      llm.Provider
	Model         string
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

	provider, model := providerFor(cfg, logger)
	toolRegistry := tools.NewRegistry(
		tools.ReadFileTool{}, tools.WriteFileTool{}, tools.ListFilesTool{},
		tools.ShellTool{}, tools.GitTool{}, tools.HTTPGetTool{},
	)
	taskTemplates := tasks.NewRegistry()
	sbox := sandbox.NewDockerSandbox(sandbox.DockerConfig{
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

	mem, useSemantic := buildMemory(cfg, st, provider, logger)

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
	}, provider, model, toolRegistry, taskTemplates, ctxbuilder.New(), arts, bus, logger)

	return &Stack{Provider: provider, Model: model, TaskTemplates: taskTemplates, Worker: w, Memory: mem}, nil
}

func providerFor(cfg config.Config, logger *slog.Logger) (llm.Provider, string) {
	if cfg.RequestyAPIKey == "" {
		logger.Warn("no REQUESTY_API_KEY set; using demo provider (offline mode)")
		return llm.DemoProvider{}, ""
	}
	return llm.NewOpenAICompatible(llm.Config{
		Name: "requesty", BaseURL: cfg.RequestyBase, APIKey: cfg.RequestyAPIKey,
		Model: cfg.LLMModel, EmbeddingModel: cfg.EmbeddingModel,
	}), cfg.LLMModel
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
