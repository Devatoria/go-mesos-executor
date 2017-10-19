package main

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/executor"
	"github.com/Devatoria/go-mesos-executor/hook"
	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	agentEndpoint           string
	cgroupsEnableCFS        bool
	containerName           string
	docker                  string
	dockerSocket            string
	executorID              string
	externalLogFile         string
	frameworkID             string
	help                    bool
	initializeDriverLogging bool
	launcherDir             string
	logBufSecs              uint
	logDir                  string
	loggingLevel            string
	mappedDirectory         string
	quiet                   bool
	sandboxDirectory        string
	stopTimeout             string
	taskEnvironment         []string
)

var rootCmd = &cobra.Command{
	Use:   "mesos-docker-executor",
	Short: "Custom Mesos Docker executor",
	Run: func(cmd *cobra.Command, args []string) {
		logger.GetInstance().Info("Initializing the executor",
			zap.String("executorID", executorID),
			zap.String("frameworkID", frameworkID),
		)

		// Prepare docker containerizer
		c, err := container.NewDockerContainerizer(dockerSocket)
		if err != nil {
			logger.GetInstance().Fatal("An error occured while initializing the containerizer",
				zap.Error(err),
			)
		}

		// Create hook manager
		hooks := viper.GetStringSlice("hooks")
		logger.GetInstance().Info("Creating hook manager",
			zap.Reflect("hooks", hooks),
		)
		m := hook.NewManager(hooks)
		m.RegisterHooks(&hook.ACLHook)
		m.RegisterHooks(&hook.IptablesHook)
		m.RegisterHooks(&hook.RemoveContainerHook)

		// Create and run the executor
		e := executor.NewExecutor(executor.Config{
			AgentEndpoint: agentEndpoint,
			ContainerName: containerName,
			ExecutorID:    executorID,
			FrameworkID:   frameworkID,
		}, c, m)
		if err := e.Execute(); err != nil {
			logger.GetInstance().Fatal("An error occured while running the executor",
				zap.Error(err),
			)
		}
	},
}

func init() {
	cobra.OnInitialize(readConfig)

	// Flags given by the agent when running th executor
	rootCmd.PersistentFlags().BoolVar(&cgroupsEnableCFS, "cgroups_enable_cfs", false, "Cgroups feature flag to enable hard limits on CPU resources via the CFS bandwidth limiting subfeature")
	rootCmd.PersistentFlags().StringVar(&containerName, "container", "", "Container name")
	rootCmd.PersistentFlags().StringVar(&docker, "docker", "", "Docker executable path (unused)")
	rootCmd.PersistentFlags().StringVar(&dockerSocket, "docker_socket", "", "Docker socket path")
	rootCmd.PersistentFlags().StringVar(&externalLogFile, "external_log_file", "", "File to log in if not using the default system")
	rootCmd.PersistentFlags().BoolVar(&help, "help", false, "Prints the help message (unused)")
	rootCmd.PersistentFlags().BoolVar(&initializeDriverLogging, "initialize_driver_logging", true, "This option has no effect when using the HTTP scheduler/executor APIs")
	rootCmd.PersistentFlags().StringVar(&launcherDir, "launcher_dir", "", "Folder from where the executor is launched")
	rootCmd.PersistentFlags().UintVar(&logBufSecs, "logbufsecs", 0, "Maximum number of seconds that logs may be buffered for.")
	rootCmd.PersistentFlags().StringVar(&logDir, "log_dir", "", "Location to put log files")
	rootCmd.PersistentFlags().StringVar(&loggingLevel, "logging_level", "", "Logging level")
	rootCmd.PersistentFlags().StringVar(&mappedDirectory, "mapped_directory", "", "The sandbox directory path that is mapped in the docker container")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "Disable logging to stderr")
	rootCmd.PersistentFlags().StringVar(&sandboxDirectory, "sandbox_directory", "", "The path to the container sandbox holding stdout and stderr files into which docker container logs will be redirected")
	rootCmd.PersistentFlags().StringVar(&stopTimeout, "stop_timeout", "", "Time to wait before killing a currently stopping container")
	rootCmd.PersistentFlags().StringSliceVar(&taskEnvironment, "task_environment", []string{}, "A JSON map of environment variables and values that should be passed into the task launched by this executor.")

	// Custom flags
	rootCmd.PersistentFlags().Bool("debug", true, "Enable debug mode")
	viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	rootCmd.PersistentFlags().StringSlice("hooks", []string{}, "Enabled hooks")
	viper.BindPFlag("hooks", rootCmd.PersistentFlags().Lookup("hooks"))
	rootCmd.PersistentFlags().String("proc_path", "/proc", "Proc mount path")
	viper.BindPFlag("proc_path", rootCmd.PersistentFlags().Lookup("proc_path"))
}

func readConfig() {
	viper.SetEnvPrefix("mesos")
	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/mesos-executor")
	viper.AddConfigPath(".")

	viper.BindEnv("agent_endpoint")
	agentEndpoint = viper.GetString("agent_endpoint")

	viper.BindEnv("executor_id")
	executorID = viper.GetString("executor_id")

	viper.BindEnv("framework_id")
	frameworkID = viper.GetString("framework_id")

	if err := viper.ReadInConfig(); err != nil {
		logger.GetInstance().Fatal("An error occured while reading the configuration file",
			zap.Error(err),
		)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		logger.GetInstance().Fatal("An error occured while running the root command",
			zap.Error(err),
		)
	}
}
