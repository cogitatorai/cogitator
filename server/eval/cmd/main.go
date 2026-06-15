package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cogitatorai/cogitator/server/eval"
	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/secretstore"
	"github.com/joho/godotenv"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "compare":
		compareCmd(os.Args[2:])
	case "cache":
		cacheCmd(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	providerName := fs.String("provider", "", "provider name (openai, anthropic, etc.)")
	model := fs.String("model", "", "model name")
	stage := fs.String("stage", "", "run single stage (enrichment, retrieval, reflection)")
	out := fs.String("out", "", "save JSON results to file")
	noCache := fs.Bool("no-cache", false, "skip response cache")
	embedderMode := fs.String("embedder", "deterministic", "retrieval embedder: deterministic | real")
	offline := fs.Bool("offline", false, "forbid live embedding API calls (cache-only)")
	embModel := fs.String("embedding-model", "", "embedding model for -embedder=real (default: config Memory.EmbeddingModel)")
	fs.Parse(args)

	godotenv.Load()

	// Load config the same way the server does (keychain, secrets.yaml, env vars).
	cfg := config.Default()
	cfg.ApplyEnv()

	ss := secretstore.NewFallbackStore(
		secretstore.NewKeychainStore(),
		secretstore.NewFileStore(os.ExpandEnv("$HOME/.cogitator")),
	)
	if sec, err := config.LoadSecretsFromStore(ss); err == nil {
		config.ApplySecrets(cfg, sec)
	}

	if *providerName == "" {
		*providerName = cfg.Models.Standard.Provider
	}
	if *model == "" {
		*model = cfg.Models.Standard.Model
	}
	if *providerName == "" || *model == "" {
		fmt.Fprintln(os.Stderr, "error: -provider and -model are required (or set COGITATOR_MODEL_PROVIDER and COGITATOR_MODEL)")
		os.Exit(1)
	}

	var prov provider.Provider
	if apiKey := firstNonEmpty(cfg.ProviderAPIKey(*providerName), os.Getenv("COGITATOR_API_KEY")); apiKey != "" {
		prov = provider.NewOpenAI(*providerName, apiKey)
	}

	dataDir := findDataDir()

	var embedder provider.Embedder
	var embeddingModel string
	switch *embedderMode {
	case "deterministic":
		embedder = eval.NewDeterministicEmbedder(0)
		embeddingModel = "deterministic"
	case "real":
		embeddingModel = *embModel
		if embeddingModel == "" {
			embeddingModel = cfg.Memory.EmbeddingModel
		}
		embDir := filepath.Join(dataDir, "embeddings")
		var inner provider.Embedder
		if !*offline {
			apiKey := firstNonEmpty(cfg.ProviderAPIKey(*providerName), os.Getenv("COGITATOR_API_KEY"))
			if apiKey == "" {
				fmt.Fprintf(os.Stderr, "error: -embedder=real without -offline needs an API key for %q (keychain, secrets.yaml, or COGITATOR_API_KEY)\n", *providerName)
				os.Exit(1)
			}
			inner = provider.NewOpenAI(*providerName, apiKey)
		}
		embedder = eval.NewCachedEmbedder(inner, embeddingModel, embDir, *offline)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown -embedder %q (want deterministic|real)\n", *embedderMode)
		os.Exit(1)
	}

	var stages []string
	if *stage != "" {
		stages = []string{*stage}
	}

	var cacheDir string
	if !*noCache {
		cacheDir = filepath.Join(dataDir, "cache")
	}

	report, err := eval.Run(context.Background(), eval.RunConfig{
		Provider:       prov,
		ProviderName:   *providerName,
		Model:          *model,
		DataDir:        dataDir,
		CacheDir:       cacheDir,
		Stages:         stages,
		Embedder:       embedder,
		EmbeddingModel: embeddingModel,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	eval.WriteTable(os.Stdout, report)

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		eval.WriteJSON(f, report)
		fmt.Fprintf(os.Stderr, "Results saved to %s\n", *out)
	}

	failed := 0
	for _, st := range report.Stages {
		for _, r := range st.Results {
			if !r.Pass {
				failed++
			}
		}
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d case(s) below threshold\n", failed)
		os.Exit(1)
	}
}

func compareCmd(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: cogitator-eval compare <file1.json> <file2.json> [...]")
		os.Exit(1)
	}

	var reports []*eval.Report
	for _, path := range args {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", path, err)
			os.Exit(1)
		}
		var r eval.Report
		if err := json.Unmarshal(data, &r); err != nil {
			fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", path, err)
			os.Exit(1)
		}
		reports = append(reports, &r)
	}

	eval.WriteComparison(os.Stdout, reports)
}

func cacheCmd(args []string) {
	if len(args) < 1 || args[0] != "clear" {
		fmt.Fprintln(os.Stderr, "usage: cogitator-eval cache clear")
		os.Exit(1)
	}
	dataDir := findDataDir()
	cache := eval.NewCache(filepath.Join(dataDir, "cache"))
	if err := cache.Clear(); err != nil {
		fmt.Fprintf(os.Stderr, "error clearing cache: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Cache cleared.")
}

func findDataDir() string {
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, "testdata")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join("testdata", "eval")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: cogitator-eval <command> [flags]

Commands:
  run       Run evaluation suite
  compare   Compare two or more result files
  cache     Manage response cache

Run flags:
  -provider   Provider name (openai, anthropic, etc.)
  -model      Model name (gpt-4o, claude-sonnet, etc.)
  -stage      Run single stage (enrichment, retrieval, reflection)
  -out        Save JSON results to file
  -no-cache   Skip response cache
  -embedder   Retrieval embedder: deterministic | real (default: deterministic)
  -offline    Forbid live embedding API calls (cache-only)
  -embedding-model  Embedding model for -embedder=real (default: config Memory.EmbeddingModel)`)
}
