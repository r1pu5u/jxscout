package main

import (
	agentcli "github.com/francisconeves97/jxscout/pkg/agent-cli"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/francisconeves97/jxscout/internal/core/common"
	"github.com/francisconeves97/jxscout/pkg/constants"
	"github.com/francisconeves97/jxscout/pkg/jxscout"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"

	"github.com/projectdiscovery/goflags"
)

const Version = constants.Version

func main() {
	workspaceRoot, workspaceConfig, hasWorkspace, workspaceErr := common.DiscoverWorkspace("")
	if workspaceErr != nil {
		log.Fatalf("failed to detect workspace: %s", workspaceErr)
	}
	if hasWorkspace && os.Getenv("JXSCOUT_DATA_DIR") == "" {
		if err := os.Setenv("JXSCOUT_PROJECT_ROOT", workspaceRoot); err != nil {
			log.Fatalf("failed to set workspace root: %s", err)
		}
	}
	if len(os.Args) == 1 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		os.Exit(agentcli.Run(nil))
	}
	serverMode := os.Args[1] == "serve"
	switch os.Args[1] {
	case "serve":
		os.Args = append(os.Args[:1], os.Args[2:]...)
	default:
		if os.Args[1][0] != '-' {
			os.Exit(agentcli.Run(os.Args[1:]))
		}
	}
	if serverMode {
		os.Args = rewriteProjectAlias(os.Args)
	}
	selectedProject, projectFlagSet := projectOption(os.Args[1:])
	if projectFlagSet && os.Getenv("JXSCOUT_DATA_DIR") == "" {
		project, found, err := common.RegisteredProject(selectedProject)
		if err != nil {
			log.Fatal(err)
		}
		if found {
			workspaceRoot = project.Root
			workspaceConfig = common.WorkspaceConfig{Version: 1, Project: selectedProject, Scope: project.Scope}
			hasWorkspace = true
			os.Setenv("JXSCOUT_PROJECT_ROOT", workspaceRoot)
		}
	}
	useWorkspaceDefaults := hasWorkspace && (!projectFlagSet || selectedProject == workspaceConfig.Project)
	scopeFlagSet := hasOption(os.Args[1:], "scope")

	options := jxscouttypes.Options{}

	flagSet := goflags.NewFlagSet()

	flagSet.SetDescription("jxscout | static files downloader for vulnerability analysis")
	flagSet.SetCustomHelpText("Select project: --project NAME (alias: -p NAME, --project-name NAME). In the TUI, use: project NAME")

	headless := false
	flagSet.BoolVar(&headless, "headless", false, "run capture without the interactive dashboard")

	flagSet.CreateGroup("server", "server configuration",
		flagSet.StringVar(&options.Hostname, constants.FlagHostname, constants.DefaultHostname, constants.DescriptionHostname),
		flagSet.IntVar(&options.Port, constants.FlagPort, constants.DefaultPort, constants.DescriptionPort),
	)

	flagSet.CreateGroup("jxscout", "jxscout configuration",
		flagSet.StringVar(&options.ProjectName, constants.FlagProjectName, constants.DefaultProjectName, constants.DescriptionProjectName),
		flagSet.StringSliceVar(&options.ScopePatterns, constants.FlagScope, nil, constants.DescriptionScope, goflags.FileCommaSeparatedStringSliceOptions),
		flagSet.BoolVar(&options.Debug, constants.FlagDebug, constants.DefaultDebug, constants.DescriptionDebug),
	)

	flagSet.CreateGroup("concurrency", "concurrency configuration",
		flagSet.IntVar(&options.ChunkParserConcurrency, "chunk-parser-concurrency", 1, "maximum concurrent chunk parsers and validators (default 1 for lower memory)"),
		flagSet.IntVar(&options.AssetFetchConcurrency, constants.FlagAssetFetchConcurrency, constants.DefaultAssetFetchConcurrency, constants.DescriptionAssetFetchConcurrency),
		flagSet.IntVar(&options.AssetSaveConcurrency, constants.FlagAssetSaveConcurrency, constants.DefaultAssetSaveConcurrency, constants.DescriptionAssetSaveConcurrency),
		flagSet.IntVar(&options.BeautifierConcurrency, constants.FlagBeautifierConcurrency, constants.DefaultBeautifierConcurrency, constants.DescriptionBeautifierConcurrency),
		flagSet.IntVar(&options.ChunkDiscovererConcurrency, constants.FlagChunkDiscovererConcurrency, constants.DefaultChunkDiscovererConcurrency, constants.DescriptionChunkDiscovererConcurrency),
		flagSet.IntVar(&options.ASTAnalyzerConcurrency, constants.FlagASTAnalyzerConcurrency, constants.DefaultASTAnalyzerConcurrency, constants.DescriptionASTAnalyzerConcurrency),
	)

	flagSet.StringVar(&options.WebcrackMode, "webcrack", "auto", "webcrack mode: auto, off, required")

	flagSet.CreateGroup("chunk discovery", "chunk discovery configuration",
		flagSet.IntVar(&options.ChunkDiscovererBruteForceLimit, constants.FlagChunkDiscovererBruteForceLimit, constants.DefaultChunkDiscovererBruteForceLimit, constants.DescriptionChunkDiscovererBruteForceLimit),
	)

	flagSet.CreateGroup("cache", "cache configuration",
		flagSet.DurationVar(&options.JavascriptRequestsCacheTTL, constants.FlagJavascriptRequestsCacheTTL, constants.DefaultJavascriptRequestsCacheTTL, constants.DescriptionJavascriptRequestsCacheTTL),
		flagSet.DurationVar(&options.HTMLRequestsCacheTTL, constants.FlagHTMLRequestsCacheTTL, constants.DefaultHTMLRequestsCacheTTL, constants.DescriptionHTMLRequestsCacheTTL),
	)

	flagSet.CreateGroup("rate limiting", "rate limiting configuration",
		flagSet.IntVar(&options.RateLimitingMaxRequestsPerSecond, constants.FlagRateLimitingMaxRequestsPerSecond, constants.DefaultRateLimitingMaxRequestsPerSecond, constants.DescriptionRateLimitingMaxRequestsPerSecond),
		flagSet.IntVar(&options.RateLimitingMaxRequestsPerMinute, constants.FlagRateLimitingMaxRequestsPerMinute, constants.DefaultRateLimitingMaxRequestsPerMinute, constants.DescriptionRateLimitingMaxRequestsPerMinute),
	)

	flagSet.CreateGroup("js ingestion", "js ingestion configuration",
		flagSet.BoolVar(&options.DownloadReferedJS, constants.FlagDownloadReferedJS, constants.DefaultDownloadReferedJS, constants.DescriptionDownloadReferedJS),
	)

	flagSet.CreateGroup("logging", "logging configuration",
		flagSet.IntVar(&options.LogBufferSize, constants.FlagLogBufferSize, constants.DefaultLogBufferSize, constants.DescriptionLogBufferSize),
		flagSet.IntVar(&options.LogFileMaxSizeMB, constants.FlagLogFileMaxSizeMB, constants.DefaultLogFileMaxSizeMB, constants.DescriptionLogFileMaxSizeMB),
	)

	flagSet.CreateGroup("overrides", "overrides configuration",
		flagSet.StringVar(&options.CaidoHostname, constants.FlagCaidoHostname, constants.DefaultCaidoHostname, constants.DescriptionCaidoHostname),
		flagSet.IntVar(&options.CaidoPort, constants.FlagCaidoPort, constants.DefaultCaidoPort, constants.DescriptionCaidoPort),
		flagSet.DurationVar(&options.OverrideContentCheckInterval, constants.FlagOverrideContentCheckInterval, constants.DefaultOverrideContentCheckInterval, constants.DescriptionOverrideContentCheckInterval),
	)

	if useWorkspaceDefaults && !projectFlagSet {
		options.ProjectName = workspaceConfig.Project
	} else if options.ProjectName == constants.DefaultProjectName && !projectFlagSet {
		projectName, err := common.GetProjectName()
		if err != nil {
			log.Fatalf("failed to get project name: %s", err.Error())
		}
		options.ProjectName = projectName
	}
	if useWorkspaceDefaults && !scopeFlagSet {
		options.ScopePatterns = workspaceConfig.Scope
	}

	if projectFlagSet {
		if err := common.ValidateProjectName(selectedProject); err != nil {
			log.Fatal(err)
		}
		options.ProjectName = selectedProject
	}

	err := jxscout.CheckAndMigrateOldVersion()
	if err != nil {
		log.Fatalf("failed to check and migrate old version: %s", err.Error())
	}

	configFileLocation := filepath.Join(common.GetPrivateDirectory(options.ProjectName), constants.ConfigFileName)
	flagSet.SetConfigFilePath(configFileLocation)

	if err := flagSet.Parse(); err != nil {
		log.Fatalf("could not parse flags: %s", err.Error())
	}

	if useWorkspaceDefaults && !projectFlagSet {
		options.ProjectName = workspaceConfig.Project
	}
	if useWorkspaceDefaults && !scopeFlagSet {
		options.ScopePatterns = workspaceConfig.Scope
	}
	if hasWorkspace && os.Getenv("JXSCOUT_DATA_DIR") == "" {
		if err := common.RegisterProject(workspaceConfig.Project, workspaceRoot, workspaceConfig.Scope); err != nil {
			log.Fatal(err)
		}
	}
	if headless {
		os.Setenv("JXSCOUT_HEADLESS", "true")
	}
	jxscout, err := jxscout.NewJXScout(options)
	if err != nil {
		flagSet.CommandLine.PrintDefaults()
		log.Fatalf("failed to initialize jxscout: %s", err.Error())
	}

	err = jxscout.Start()
	if err != nil {
		log.Fatalf("failed to start jxscout: %s", err.Error())
	}
}

func hasOption(args []string, names ...string) bool {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	for _, arg := range args {
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if _, ok := wanted[name]; ok {
			return true
		}
	}
	return false
}

func rewriteProjectAlias(args []string) []string {
	rewritten := append([]string(nil), args...)
	for i, arg := range rewritten {
		if arg == "-p" || strings.HasPrefix(arg, "-p=") {
			rewritten[i] = "--project-name" + strings.TrimPrefix(arg, "-p")
			continue
		}
		if arg == "--project" || arg == "-project" {
			rewritten[i] = strings.Replace(arg, "project", "project-name", 1)
			continue
		}
		if strings.HasPrefix(arg, "--project=") || strings.HasPrefix(arg, "-project=") {
			rewritten[i] = strings.Replace(arg, "project=", "project-name=", 1)
		}
	}
	return rewritten
}

// projectOption selects the config file before goflags merges it with CLI flags.
func projectOption(args []string) (string, bool) {
	var project string
	found := false
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if name != "project-name" && name != "project" && name != "p" {
			continue
		}
		found = true
		if inline {
			project = value
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			project = args[i]
		} else {
			project = ""
		}
	}
	return project, found
}
