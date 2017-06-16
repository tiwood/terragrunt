package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gruntwork-io/terragrunt/errors"
	"github.com/gruntwork-io/terragrunt/options"
	"github.com/gruntwork-io/terragrunt/remote"
	"github.com/gruntwork-io/terragrunt/util"
	"github.com/hashicorp/hcl"
)

const DefaultTerragruntConfigPath = "terraform.tfvars"
const OldTerragruntConfigPath = ".terragrunt"

// TerragruntConfig represents a parsed and expanded configuration
type TerragruntConfig struct {
	Terraform    *TerraformConfig
	RemoteState  *remote.RemoteState
	Dependencies *ModuleDependencies
}

func (conf *TerragruntConfig) String() string {
	return fmt.Sprintf("TerragruntConfig{Terraform = %v, RemoteState = %v, Dependencies = %v}", conf.Terraform, conf.RemoteState, conf.Dependencies)
}

// terragruntConfigFile represents the configuration supported in a Terragrunt configuration file (i.e.
// terraform.tfvars or .terragrunt)
type terragruntConfigFile struct {
	Terraform    *TerraformConfig    `hcl:"terraform,omitempty"`
	Include      *IncludeConfig      `hcl:"include,omitempty"`
	Lock         *LockConfig         `hcl:"lock,omitempty"`
	RemoteState  *remote.RemoteState `hcl:"remote_state,omitempty"`
	Dependencies *ModuleDependencies `hcl:"dependencies,omitempty"`
}

// Older versions of Terraform did not support locking, so Terragrunt offered locking as a feature. As of version 0.9.0,
// Terraform supports locking natively, so this feature was removed from Terragrunt. However, we keep around the
// LockConfig so we can log a warning for Terragrunt users who are still trying to use it.
type LockConfig map[interface{}]interface{}

// tfvarsFileWithTerragruntConfig represents a .tfvars file that contains a terragrunt = { ... } block
type tfvarsFileWithTerragruntConfig struct {
	Terragrunt *terragruntConfigFile `hcl:"terragrunt,omitempty"`
}

// IncludeConfig represents the configuration settings for a parent Terragrunt configuration file that you can
// "include" in a child Terragrunt configuration file
type IncludeConfig struct {
	Path string `hcl:"path"`
}

// ModuleDependencies represents the paths to other Terraform modules that must be applied before the current module
// can be applied
type ModuleDependencies struct {
	Paths []string `hcl:"paths"`
}

func (deps *ModuleDependencies) String() string {
	return fmt.Sprintf("ModuleDependencies{Paths = %v}", deps.Paths)
}

// TerraformConfig specifies where to find the Terraform configuration files
type TerraformConfig struct {
	ExtraArgs []TerraformExtraArguments `hcl:"extra_arguments"`
	Source    string                    `hcl:"source"`
}

func (conf *TerraformConfig) String() string {
	return fmt.Sprintf("TerraformConfig{Source = %v}", conf.Source)
}

// TerraformExtraArguments sets a list of arguments to pass to Terraform if command fits any in the `Commands` list
type TerraformExtraArguments struct {
	Name             string   `hcl:",key"`
	Arguments        []string `hcl:"arguments,omitempty"`
	RequiredVarFiles []string `hcl:"required_var_files,omitempty"`
	OptionalVarFiles []string `hcl:"optional_var_files,omitempty"`
	Commands         []string `hcl:"commands,omitempty"`
}

func (conf *TerraformExtraArguments) String() string {
	return fmt.Sprintf("TerraformArguments{Name = %s, Arguments = %v, Commands = %v}", conf.Name, conf.Arguments, conf.Commands)
}

// Return the default path to use for the Terragrunt configuration file. The reason this is a method rather than a
// constant is that older versions of Terragrunt stored configuration in a different file. This method returns the
// path to the old configuration format if such a file exists and the new format otherwise.
func DefaultConfigPath(workingDir string) string {
	path := util.JoinPath(workingDir, OldTerragruntConfigPath)
	if util.FileExists(path) {
		return path
	}
	return util.JoinPath(workingDir, DefaultTerragruntConfigPath)
}

// Returns a list of all Terragrunt config files in the given path or any subfolder of the path. A file is a Terragrunt
// config file if it has a name as returned by the DefaultConfigPath method and contains Terragrunt config contents
// as returned by the IsTerragruntConfigFile method.
func FindConfigFilesInPath(rootPath string) ([]string, error) {
	configFiles := []string{}

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			configPath := DefaultConfigPath(path)
			isTerragruntConfig, err := IsTerragruntConfigFile(configPath)
			if err != nil {
				return err
			}
			if isTerragruntConfig {
				configFiles = append(configFiles, configPath)
			}
		}

		return nil
	})

	return configFiles, err
}

// Returns true if the given path corresponds to file that could be a Terragrunt config file. A file could be a
// Terragrunt config file if:
//
// 1. The file exists
// 2. It is a .terragrunt file, which is the old Terragrunt-specific file format
// 3. The file contains HCL contents with a terragrunt = { ... } block
func IsTerragruntConfigFile(path string) (bool, error) {
	if !util.FileExists(path) {
		return false, nil
	}

	if isOldTerragruntConfig(path) {
		return true, nil
	}

	return isNewTerragruntConfig(path)
}

// Returns true if the given path points to an old Terragrunt config file
func isOldTerragruntConfig(path string) bool {
	return strings.HasSuffix(path, OldTerragruntConfigPath)
}

// Retrusn true if the given path points to a new (current) Terragrunt config file
func isNewTerragruntConfig(path string) (bool, error) {
	configContents, err := util.ReadFileAsString(path)
	if err != nil {
		return false, err
	}

	return containsTerragruntBlock(configContents), nil
}

// Returns true if the given string contains valid HCL with a terragrunt = { ... } block
func containsTerragruntBlock(configString string) bool {
	terragruntConfig := &tfvarsFileWithTerragruntConfig{}
	if err := hcl.Decode(terragruntConfig, configString); err != nil {
		return false
	}
	return terragruntConfig.Terragrunt != nil
}

// Read the Terragrunt config file from its default location
func ReadTerragruntConfig(terragruntOptions *options.TerragruntOptions) (*TerragruntConfig, error) {
	terragruntOptions.Logger.Printf("Reading Terragrunt config file at %s", terragruntOptions.TerragruntConfigPath)
	return ParseConfigFile(terragruntOptions.TerragruntConfigPath, terragruntOptions, nil)
}

// Parse the Terragrunt config file at the given path. If the include parameter is not nil, then treat this as a config
// included in some other config file when resolving relative paths.
func ParseConfigFile(configPath string, terragruntOptions *options.TerragruntOptions, include *IncludeConfig) (*TerragruntConfig, error) {
	if isOldTerragruntConfig(configPath) {
		terragruntOptions.Logger.Printf("DEPRECATION WARNING: Found deprecated config file format %s. This old config format will not be supported in the future. Please move your config files into a %s file.", configPath, DefaultTerragruntConfigPath)
	}

	configString, err := util.ReadFileAsString(configPath)
	if err != nil {
		return nil, err
	}

	config, err := parseConfigString(configString, terragruntOptions, include, configPath)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// Parse the Terragrunt config contained in the given string.
func parseConfigString(configString string, terragruntOptions *options.TerragruntOptions, include *IncludeConfig, configPath string) (*TerragruntConfig, error) {
	resolvedConfigString, err := ResolveTerragruntConfigString(configString, include, terragruntOptions)
	if err != nil {
		return nil, err
	}

	terragruntConfigFile, err := parseConfigStringAsTerragruntConfigFile(resolvedConfigString, configPath)
	if err != nil {
		return nil, err
	}
	if terragruntConfigFile == nil {
		return nil, errors.WithStackTrace(CouldNotResolveTerragruntConfigInFile(configPath))
	}

	config, err := convertToTerragruntConfig(terragruntConfigFile, terragruntOptions)
	if err != nil {
		return nil, err
	}

	if include != nil && terragruntConfigFile.Include != nil {
		return nil, errors.WithStackTrace(TooManyLevelsOfInheritance{
			ConfigPath:             terragruntOptions.TerragruntConfigPath,
			FirstLevelIncludePath:  include.Path,
			SecondLevelIncludePath: terragruntConfigFile.Include.Path,
		})
	}

	includedConfig, err := parseIncludedConfig(terragruntConfigFile.Include, terragruntOptions)
	if err != nil {
		return nil, err
	}

	return mergeConfigWithIncludedConfig(config, includedConfig, terragruntOptions)
}

// Parse the given config string, read from the given config file, as a terragruntConfigFile struct. This method solely
// converts the HCL syntax in the string to the terragruntConfigFile struct; it does not process any interpolations.
func parseConfigStringAsTerragruntConfigFile(configString string, configPath string) (*terragruntConfigFile, error) {
	if isOldTerragruntConfig(configPath) {
		terragruntConfig := &terragruntConfigFile{}
		if err := hcl.Decode(terragruntConfig, configString); err != nil {
			return nil, errors.WithStackTrace(err)
		}
		return terragruntConfig, nil
	} else {
		tfvarsConfig := &tfvarsFileWithTerragruntConfig{}
		if err := hcl.Decode(tfvarsConfig, configString); err != nil {
			return nil, errors.WithStackTrace(err)
		}
		return tfvarsConfig.Terragrunt, nil
	}
}

// Merge the given config with an included config. Anything specified in the current config will override the contents
// of the included config. If the included config is nil, just return the current config.
func mergeConfigWithIncludedConfig(config *TerragruntConfig, includedConfig *TerragruntConfig, terragruntOptions *options.TerragruntOptions) (*TerragruntConfig, error) {
	if includedConfig == nil {
		return config, nil
	}

	if config.RemoteState != nil {
		includedConfig.RemoteState = config.RemoteState
	}

	if config.Terraform != nil {
		if includedConfig.Terraform == nil {
			includedConfig.Terraform = config.Terraform
		} else {
			if config.Terraform.Source != "" {
				includedConfig.Terraform.Source = config.Terraform.Source
			}
			mergeExtraArgs(terragruntOptions, config.Terraform.ExtraArgs, &includedConfig.Terraform.ExtraArgs)
		}
	}

	if config.Dependencies != nil {
		includedConfig.Dependencies = config.Dependencies
	}

	return includedConfig, nil
}

// Merge the extra arguments prioritizing those defined in the childExtraArgs
func mergeExtraArgs(terragruntOptions *options.TerragruntOptions, childExtraArgs []TerraformExtraArguments, parentExtraArgs *[]TerraformExtraArguments) {
	result := *parentExtraArgs
addExtra:
	for _, child := range childExtraArgs {
		for i, parent := range result {
			if parent.Name == child.Name {
				terragruntOptions.Logger.Printf("extra_arguments '%v' from child overriding parent", child.Name)
				// For extra args, we want to keep the values specified in the child and put them after
				// the parent ones, so if we encounter a duplicate, we just overwrite it.
				result[i] = child
				continue addExtra
			}
		}
		result = append(result, child)
	}
	*parentExtraArgs = result
}

// Parse the config of the given include, if one is specified
func parseIncludedConfig(includedConfig *IncludeConfig, terragruntOptions *options.TerragruntOptions) (*TerragruntConfig, error) {
	if includedConfig == nil {
		return nil, nil
	}
	if includedConfig.Path == "" {
		return nil, errors.WithStackTrace(IncludedConfigMissingPath(terragruntOptions.TerragruntConfigPath))
	}

	resolvedIncludePath, err := ResolveTerragruntConfigString(includedConfig.Path, nil, terragruntOptions)
	if err != nil {
		return nil, err
	}

	if !filepath.IsAbs(resolvedIncludePath) {
		resolvedIncludePath = util.JoinPath(filepath.Dir(terragruntOptions.TerragruntConfigPath), resolvedIncludePath)
	}

	return ParseConfigFile(resolvedIncludePath, terragruntOptions, includedConfig)
}

// Convert the contents of a fully resolved Terragrunt configuration to a TerragruntConfig object
func convertToTerragruntConfig(terragruntConfigFromFile *terragruntConfigFile, terragruntOptions *options.TerragruntOptions) (*TerragruntConfig, error) {
	terragruntConfig := &TerragruntConfig{}

	if terragruntConfigFromFile.Lock != nil {
		terragruntOptions.Logger.Printf("WARNING: Found a lock configuration in the Terraform configuration at %s. Terraform added native support for locking as of version 0.9.0, so this feature has been removed from Terragrunt and will have no effect. See your Terraform backend docs for how to configure locking: https://www.terraform.io/docs/backends/types/index.html.", terragruntOptions.TerragruntConfigPath)
	}

	if terragruntConfigFromFile.RemoteState != nil {
		terragruntConfigFromFile.RemoteState.FillDefaults()
		if err := terragruntConfigFromFile.RemoteState.Validate(); err != nil {
			return nil, err
		}

		terragruntConfig.RemoteState = terragruntConfigFromFile.RemoteState
	}

	terragruntConfig.Terraform = terragruntConfigFromFile.Terraform
	terragruntConfig.Dependencies = terragruntConfigFromFile.Dependencies

	return terragruntConfig, nil
}

// Custom error types

type IncludedConfigMissingPath string

func (err IncludedConfigMissingPath) Error() string {
	return fmt.Sprintf("The include configuration in %s must specify a 'path' parameter", string(err))
}

type TooManyLevelsOfInheritance struct {
	ConfigPath             string
	FirstLevelIncludePath  string
	SecondLevelIncludePath string
}

func (err TooManyLevelsOfInheritance) Error() string {
	return fmt.Sprintf("%s includes %s, which itself includes %s. Only one level of includes is allowed.", err.ConfigPath, err.FirstLevelIncludePath, err.SecondLevelIncludePath)
}

type CouldNotResolveTerragruntConfigInFile string

func (err CouldNotResolveTerragruntConfigInFile) Error() string {
	return fmt.Sprintf("Could not find Terragrunt configuration settings in %s", string(err))
}
