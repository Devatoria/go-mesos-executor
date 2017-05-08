package main

import (
	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/executor"

	"github.com/Sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	containerName           string
	docker                  string
	dockerSocket            string
	executorID              string
	frameworkID             string
	help                    bool
	initializeDriverLogging bool
	launcherDir             string
	logBufSecs              uint
	loggingLevel            string
	mappedDirectory         string
	quiet                   bool
	sandboxDirectory        string
	stopTimeout             string
)

var rootCmd = &cobra.Command{
	Use:   "mesos-docker-executor",
	Short: "Custom Mesos Docker executor",
	Run: func(cmd *cobra.Command, args []string) {
		logrus.WithFields(logrus.Fields{"ExecutorID": executorID, "FrameworkID": frameworkID}).Info("Initializing an executor")

		// Prepare docker containerizer
		c, err := container.NewDockerContainerizer(dockerSocket)
		if err != nil {
			logrus.Fatal("Unable to initialize containerizer: ", err)
		}

		// Create and run the executor
		e := executor.NewExecutor(executorID, frameworkID, c)
		if err := e.Execute(); err != nil {
			logrus.Fatal("Error while running executor: ", err)
		}
	},
}

func init() {
	cobra.OnInitialize(readConfig)

	// Flags given by the agent when running th executor
	rootCmd.PersistentFlags().StringVar(&containerName, "container", "", "Container name")
	rootCmd.PersistentFlags().StringVar(&docker, "docker", "", "???")
	rootCmd.PersistentFlags().StringVar(&dockerSocket, "docker_socket", "", "Docker socket path")
	rootCmd.PersistentFlags().BoolVar(&help, "help", false, "???")
	rootCmd.PersistentFlags().BoolVar(&initializeDriverLogging, "initialize_driver_logging", true, "???")
	rootCmd.PersistentFlags().StringVar(&launcherDir, "launcher_dir", "", "Folder from where the executor is launched")
	rootCmd.PersistentFlags().UintVar(&logBufSecs, "logbufsecs", 0, "???")
	rootCmd.PersistentFlags().StringVar(&loggingLevel, "logging_level", "", "Logging level")
	rootCmd.PersistentFlags().StringVar(&mappedDirectory, "mapped_directory", "", "Mesos mapped directory to mount (eg. sandbox)")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "???")
	rootCmd.PersistentFlags().StringVar(&sandboxDirectory, "sandbox_directory", "", "Mesos sandbox directory to mount")
	rootCmd.PersistentFlags().StringVar(&stopTimeout, "stop_timeout", "", "Timeout used to stop the container")
}

func readConfig() {
	viper.SetEnvPrefix("mesos")
	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/mesos-executor")
	viper.AddConfigPath(".")

	viper.BindEnv("executor_id")
	executorID = viper.GetString("executor_id")

	viper.BindEnv("framework_id")
	frameworkID = viper.GetString("framework_id")

	if err := viper.ReadInConfig(); err != nil {
		logrus.Fatal("Unable to read configuration file: ", err)
	}
}

func main() {
	logrus.SetLevel(logrus.InfoLevel)
	if err := rootCmd.Execute(); err != nil {
		logrus.Fatal("Unable to execute root command: ", err)
	}
}
