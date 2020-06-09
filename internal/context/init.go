package context

import (
	"os"
	"path"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/commitdev/zero/internal/config/globalconfig"
	"github.com/commitdev/zero/internal/config/moduleconfig"
	"github.com/commitdev/zero/internal/config/projectconfig"
	"github.com/commitdev/zero/internal/module"
	project "github.com/commitdev/zero/pkg/credentials"
	"github.com/commitdev/zero/pkg/util/exit"
	"github.com/commitdev/zero/pkg/util/flog"
	"github.com/manifoldco/promptui"
)

type Registry map[string][]string

// Create cloud provider context
func Init(outDir string) *projectconfig.ZeroProjectConfig {
	projectConfig := defaultProjConfig()

	projectConfig.Name = getProjectNamePrompt().GetParam(projectConfig.Parameters)

	rootDir := path.Join(outDir, projectConfig.Name)
	flog.Infof(":tada: Creating project")

	err := os.MkdirAll(rootDir, os.ModePerm)
	if os.IsExist(err) {
		exit.Fatal("Directory %v already exists! Error: %v", projectConfig.Name, err)
	} else if err != nil {
		exit.Fatal("Error creating root: %v ", err)
	}

	prompts := getProjectPrompts(projectConfig.Name)
	projectConfig.Parameters["ShouldPushRepoUpstream"] = prompts["ShouldPushRepoUpstream"].GetParam(projectConfig.Parameters)
	// Prompting for push-up stream, then conditionally prompting for github
	projectConfig.Parameters["GithubRootOrg"] = prompts["GithubRootOrg"].GetParam(projectConfig.Parameters)
	personalToken := prompts["githubPersonalToken"].GetParam(projectConfig.Parameters)
	if personalToken != "" && personalToken != globalconfig.GetUserCredentials(projectConfig.Name).AccessToken {
		projectConfig.Parameters["githubPersonalToken"] = personalToken
		projectCredential := globalconfig.GetUserCredentials(projectConfig.Name)
		projectCredential.GithubResourceConfig.AccessToken = personalToken
		globalconfig.Save(projectCredential)
	}
	moduleSources := chooseStack(getRegistry())
	moduleConfigs := loadAllModules(moduleSources)
	for _ = range moduleConfigs {
		// TODO: initialize module structs inside project
	}

	projectParameters := promptAllModules(moduleConfigs)
	for k, v := range projectParameters {
		projectConfig.Parameters[k] = v
		// TODO: Add parameters to module structs inside project
	}

	// TODO: load ~/.zero/config.yml (or credentials)
	// TODO: prompt global credentials

	return &projectConfig
}

// loadAllModules takes a list of module sources, downloads those modules, and parses their config
func loadAllModules(moduleSources []string) map[string]moduleconfig.ModuleConfig {
	modules := make(map[string]moduleconfig.ModuleConfig)

	for _, moduleSource := range moduleSources {
		mod, err := module.FetchModule(moduleSource)
		if err != nil {
			exit.Fatal("Unable to load module:  %v\n", err)
		}
		modules[mod.Name] = mod
	}
	return modules
}

// promptAllModules takes a map of all the modules and prompts the user for values for all the parameters
func promptAllModules(modules map[string]moduleconfig.ModuleConfig) map[string]string {
	parameterValues := make(map[string]string)
	for _, config := range modules {
		var err error
		parameterValues, err = PromptModuleParams(config, parameterValues)
		if err != nil {
			exit.Fatal("Exiting prompt:  %v\n", err)
		}
	}
	return parameterValues
}

// Project name is prompt individually because the rest of the prompts
// requires the projectName to populate defaults
func getProjectNamePrompt() PromptHandler {
	return PromptHandler{
		moduleconfig.Parameter{
			Field:   "projectName",
			Label:   "Project Name",
			Default: "",
		},
		NoCondition,
	}
}

func getProjectPrompts(projectName string) map[string]PromptHandler {
	return map[string]PromptHandler{
		"ShouldPushRepoUpstream": {
			moduleconfig.Parameter{
				Field:   "ShouldPushRepoUpstream",
				Label:   "Should the created projects be checked into github automatically? (y/n)",
				Default: "y",
			},
			NoCondition,
		},
		"GithubRootOrg": {
			moduleconfig.Parameter{
				Field:   "GithubRootOrg",
				Label:   "What's the root of the github org to create repositories in?",
				Default: "github.com/",
			},
			KeyMatchCondition("ShouldPushRepoUpstream", "y"),
		},
		"githubPersonalToken": {
			moduleconfig.Parameter{
				Field:   "githubPersonalToken",
				Label:   "Github Personal Access Token with access to the above organization",
				Default: globalconfig.GetUserCredentials(projectName).AccessToken,
			},
			KeyMatchCondition("ShouldPushRepoUpstream", "y"),
		},
	}
}

func chooseCloudProvider(projectConfig *projectconfig.ZeroProjectConfig) {
	// @TODO move options into configs
	providerPrompt := promptui.Select{
		Label: "Select Cloud Provider",
		Items: []string{"Amazon AWS", "Google GCP", "Microsoft Azure"},
	}

	_, providerResult, err := providerPrompt.Run()
	if err != nil {
		exit.Fatal("Prompt failed %v\n", err)
	}

	if providerResult != "Amazon AWS" {
		exit.Fatal("Only the AWS provider is available at this time")
	}
}

func getRegistry() Registry {
	return Registry{
		// TODO: better place to store these options as configuration file or any source
		"EKS + Go + React": []string{
			"github.com/commitdev/zero-aws-eks-stack",
			"github.com/commitdev/zero-deployable-backend",
			"github.com/commitdev/zero-deployable-react-frontend",
		},
		"Custom": []string{},
	}
}

func (registry Registry) availableLabels() []string {
	labels := make([]string, len(registry))
	i := 0
	for label := range registry {
		labels[i] = label
		i++
	}
	return labels
}

func chooseStack(registry Registry) []string {
	providerPrompt := promptui.Select{
		Label: "Pick a stack you'd like to use",
		Items: registry.availableLabels(),
	}
	_, providerResult, err := providerPrompt.Run()
	if err != nil {
		exit.Fatal("Prompt failed %v\n", err)
	}
	return registry[providerResult]

}

func fillProviderDetails(projectConfig *projectconfig.ZeroProjectConfig, s project.Secrets) {
	if projectConfig.Infrastructure.AWS != nil {
		sess, err := session.NewSession(&aws.Config{
			Region:      aws.String(projectConfig.Infrastructure.AWS.Region),
			Credentials: credentials.NewStaticCredentials(s.AWS.AccessKeyID, s.AWS.SecretAccessKey, ""),
		})

		svc := sts.New(sess)
		input := &sts.GetCallerIdentityInput{}

		awsCaller, err := svc.GetCallerIdentity(input)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				default:
					exit.Error(aerr.Error())
				}
			} else {
				exit.Error(err.Error())
			}
		}

		if awsCaller != nil && awsCaller.Account != nil {
			projectConfig.Infrastructure.AWS.AccountID = *awsCaller.Account
		}
	}
}

func defaultProjConfig() projectconfig.ZeroProjectConfig {
	return projectconfig.ZeroProjectConfig{
		Name: "",
		Infrastructure: projectconfig.Infrastructure{
			AWS: nil,
		},
		Parameters: map[string]string{},
		Modules:    []string{},
	}
}