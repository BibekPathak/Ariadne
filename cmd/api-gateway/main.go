package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"kubeai/internal/agents"
	"kubeai/internal/artifacts"
	"kubeai/internal/config"
	ctxbuilder "kubeai/internal/context"
	"kubeai/internal/events"
	"kubeai/internal/llm"
	"kubeai/internal/planner"
	"kubeai/internal/sandbox"
	"kubeai/internal/scheduler"
	"kubeai/internal/store"
	"kubeai/internal/tasks"
	"kubeai/internal/tools"
	"kubeai/internal/worker"
	"kubeai/internal/workflow"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := store.Migrate(cfg.DatabaseURL); err != nil {
			logger.Error("migration failed", "err", err)
			os.Exit(1)
		}
		logger.Info("migrations applied")
		return
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	bus := events.NewInMemoryBus(logger)
	bus.SetPersister(func(e events.Event) (int64, error) {
		return st.Events.Append(ctx, e)
	})
	defer bus.Close()

	service := wire(ctx, cfg, st, bus, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           routes(service, bus, cfg.Timeout, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("control plane listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

// wire assembles the Phase 1 control plane in-process. This is the exact
// seam that Phase 4 splits across processes: bus becomes NATS, worker becomes
// a remote executor.
func wire(ctx context.Context, cfg config.Config, st *store.Store, bus events.EventBus, logger *slog.Logger) *agents.AgentService {
	arts, err := artifacts.New(cfg.ArtifactsDir, st.Artifacts)
	if err != nil {
		logger.Error("init artifact store", "err", err)
		os.Exit(1)
	}

	var provider llm.Provider
	var model string
	if cfg.RequestyAPIKey == "" {
		provider = llm.DemoProvider{}
		logger.Warn("no REQUESTY_API_KEY set; using demo provider (offline mode)")
	} else {
		provider = llm.NewOpenAICompatible(llm.Config{
			Name: "requesty", BaseURL: cfg.RequestyBase, APIKey: cfg.RequestyAPIKey, Model: cfg.LLMModel,
		})
		model = cfg.LLMModel
	}

	toolRegistry := tools.NewRegistry(
		tools.ReadFileTool{}, tools.WriteFileTool{}, tools.ListFilesTool{},
		tools.ShellTool{}, tools.GitTool{}, tools.HTTPGetTool{},
	)
	taskTemplates := tasks.NewRegistry()
	agentTemplates := agents.NewTemplateRegistry()

	var plannerIf planner.Planner
	if cfg.RequestyAPIKey == "" {
		plannerIf = planner.StaticPlanner{}
	} else {
		plannerIf = planner.NewLLMPlanner(provider, taskTemplates, model)
	}

	sbox := sandbox.NewDockerSandbox(sandbox.DockerConfig{
		Image:   cfg.SandboxImage,
		CPU:     cfg.SandboxCPU,
		MemMB:   cfg.SandboxMemMB,
		Network: cfg.SandboxNetwork,
		BaseDir: cfg.RepoBaseDir,
	})

	w := worker.New(worker.Config{
		MaxIterations: cfg.MaxIterations,
		RepoBaseDir:   cfg.RepoBaseDir,
		Sandbox:       sbox,
	}, provider, model, toolRegistry, taskTemplates, ctxbuilder.New(), arts, bus, logger)

	sched := scheduler.NewScheduler(bus, w, logger)
	compiler := workflow.NewCompiler(taskTemplates)
	engine := workflow.NewEngine(st.Tasks, bus, logger, 1)
	return agents.NewAgentService(st, bus, agentTemplates, plannerIf, compiler, engine, sched, logger)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
