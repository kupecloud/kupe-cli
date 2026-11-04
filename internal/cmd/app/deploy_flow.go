package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cluster"
	"github.com/kupecloud/kupe-cli/internal/cmd/registry"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type deployCreated struct {
	App         bool `json:"app" yaml:"app"`
	Environment bool `json:"environment" yaml:"environment"`
	Cluster     bool `json:"cluster" yaml:"cluster"`
}

type deployResult struct {
	App         string        `json:"app" yaml:"app"`
	Environment string        `json:"environment" yaml:"environment"`
	Cluster     string        `json:"cluster" yaml:"cluster"`
	Release     string        `json:"release" yaml:"release"`
	Image       string        `json:"image" yaml:"image"`
	Address     string        `json:"address" yaml:"address"`
	Created     deployCreated `json:"created" yaml:"created"`
}

type deployPresentation struct {
	dryRun  bool
	review  func(*client.AppEnvironment, *client.AppWeb, []string)
	created deployCreated
}

func (p *deployPresentation) render(f *cli.Factory, format *printer.Format, app, env string, item *client.AppEnvironment) error {
	if item == nil || item.Web == nil {
		return cli.New(cli.ExitUnavailable, "deployment returned no environment configuration")
	}
	result := deployResult{App: app, Environment: env, Cluster: item.Cluster, Image: item.Web.Image, Created: p.created}
	if item.Status != nil {
		result.Address = item.Status.Address
		result.Release = requestedRelease(item)
	}
	switch format.Kind {
	case printer.JSON:
		return printer.PrintJSON(f.IOStreams.Out, result)
	case printer.YAML, printer.Template, printer.JSONPath, printer.Name:
		return printer.AppEnvironment.One(f.IOStreams, format, item)
	default:
		_, err := fmt.Fprintln(f.IOStreams.Out, result.Address)
		return err
	}
}

type deployLookupError struct{ error }

func (e *deployLookupError) Unwrap() error { return e.error }

// A disabled Apps API also answers 404, but must never start creation.
func deployMissing(err error) bool {
	return client.IsNotFound(err) && client.ErrorCode(err) != "apps_unavailable"
}

func newDeployCmd(f *cli.Factory) *cobra.Command {
	o := &envOptions{}
	p := &deployPresentation{}
	var yes, createCluster bool
	cmd := deployUpdateCommand(f, o, &yes, p)
	update := cmd.RunE
	cmd.Use = "deploy [APP [ENV]] [--image IMAGE]"
	cmd.Args = cobra.RangeArgs(0, 2)
	cmd.Flags().Lookup("image").Usage = "Image tag or digest reference"
	cmd.Long = `Deploy an image, creating its app and environment when needed.
With no arguments in a terminal, choose an image, app, environment and cluster.
Flags provide the same inputs without questions. New environments default to
production. A missing cluster is created only with --create-cluster or an
explicit interactive choice. Every write follows one review and confirmation.

An existing environment keeps its configuration unless --image, --set-env,
--port or --replicas changes it. Use app env update for other changes.
--dry-run checks and reviews the configuration without writing anything.
By default, wait for this release's outcome; recovery still counts as failure.
Success prints the HTTPS address, or one compact result with -o json.`
	cmd.Flags().BoolVar(&p.dryRun, "dry-run", false, "Check and review the deployment without writing")
	cmd.Flags().BoolVar(&createCluster, "create-cluster", false, "Create the named cluster if missing (default dev)")
	cmd.Flags().StringVar(&o.cluster, "cluster", "", "Cluster for a new environment")
	cmd.Flags().StringArrayVar(&o.secretEnv, "secret-env", nil, envReferenceHelp)
	cmd.Flags().StringArrayVar(&o.secrets, "secret", nil, "Declare a Vault reference: NAME=VAULT_PATH")
	cmd.Flags().StringArrayVar(&o.domains, "domain", nil, "Add an exact hostname")
	cmd.Flags().StringVar(&o.readiness, "readiness", "", "Readiness check: tcp or http:/path")
	cmd.Flags().StringVar(&o.cpu, "cpu", "", "CPU request per replica")
	cmd.Flags().StringVar(&o.memory, "memory", "", "Memory request per replica")
	cmd.Flags().StringVar(&o.cpuLimit, "cpu-limit", "", "CPU limit per replica")
	cmd.Flags().StringVar(&o.memoryLimit, "memory-limit", "", "Memory limit per replica")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		// Preserve the reviewed RMW path, its single read and preconditions for the
		// established command form. Only a missing lookup may enter creation.
		if len(args) > 0 && o.image != "" && !newDeployFlags(cmd) && !p.dryRun && (!interactiveDeploy(f.IOStreams) || len(args) >= 2) {
			err := update(cmd, args)
			var missing *deployLookupError
			if !errors.As(err, &missing) {
				return err
			}
		}
		return runDeployFlow(cmd, f, o, &yes, createCluster, p, update, args)
	}
	return cmd
}

func newDeployFlags(cmd *cobra.Command) bool {
	for _, name := range []string{"cluster", "create-cluster", "secret-env", "secret", "domain", "readiness", "cpu", "memory", "cpu-limit", "memory-limit"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func interactiveDeploy(streams *cli.IOStreams) bool {
	return streams.PromptsEnabled && streams.IsStdinTTY() && streams.IsStdoutTTY()
}

func runDeployFlow(cmd *cobra.Command, f *cli.Factory, o *envOptions, yes *bool, createCluster bool, p *deployPresentation, update func(*cobra.Command, []string) error, args []string) error {
	if o.waitTimeout <= 0 {
		return cli.MisuseError("--wait-timeout must be positive")
	}
	api, err := f.Client()
	if err != nil {
		return err
	}
	interactive := interactiveDeploy(f.IOStreams)
	if o.image == "" {
		if !interactive {
			return cli.MisuseError("pass --image to deploy")
		}
		o.image, err = chooseDeployImage(cmd.Context(), f.IOStreams, api)
		if err != nil {
			return err
		}
	}
	if err := validateImageInput(o.image); err != nil {
		return err
	}
	source := o.image
	appName := deriveAppName(source)
	if len(args) > 0 {
		appName = args[0]
	} else if interactive {
		appName, err = cli.PromptLine(cmd.Context(), f.IOStreams, "App", appName)
		if err != nil {
			return err
		}
	}
	if err := validateName(appName, "app", 32); err != nil {
		return err
	}
	_, _, err = api.Apps().Get(cmd.Context(), appName)
	appNew := deployMissing(err)
	if err != nil && !appNew {
		return translateErr(err, "", appName)
	}
	envName := ""
	if len(args) > 1 {
		envName = args[1]
	} else {
		var environments []client.AppEnvironment
		if !appNew {
			environments, err = api.AppEnvironments(appName).List(cmd.Context())
			if err != nil {
				return translateErr(err, appName, "")
			}
		}
		switch len(environments) {
		case 0:
			envName = "production"
		case 1:
			envName = environments[0].Environment
		default:
			if !interactive {
				return cli.MisuseError("pass ENV to choose an environment")
			}
			choices := make([]string, len(environments))
			for i, e := range environments {
				choices[i] = e.Environment
			}
			sort.Strings(choices)
			envName, err = chooseDeployValue(cmd.Context(), f.IOStreams, "Environment", choices)
			if err != nil {
				return err
			}
		}
		if interactive && len(environments) <= 1 {
			envName, err = cli.PromptLine(cmd.Context(), f.IOStreams, "Environment", envName)
			if err != nil {
				return err
			}
		}
	}
	if err := validateName(envName, "environment", 20); err != nil {
		return err
	}
	var current *client.AppEnvironment
	if !appNew {
		current, _, err = api.AppEnvironments(appName).Get(cmd.Context(), envName)
		if err != nil && !deployMissing(err) {
			return translateErr(err, appName, envName)
		}
	}
	if current != nil {
		if err := existingDeployFlags(cmd, o, current); err != nil {
			return err
		}
		if err := cmd.Flags().Set("image", o.image); err != nil {
			return err
		}
		p.review = func(e *client.AppEnvironment, web *client.AppWeb, changed []string) {
			reviewDeployPlan(f.IOStreams, appName, envName, e.Cluster, api.Tenant(), false, false, false, e.Status)
			reviewRelease(f.IOStreams, "Review deploy", appName, envName, source, e, web, changed)
		}
		return update(cmd, []string{appName, envName})
	}
	resolution, err := api.ResolveAppImage(cmd.Context(), source)
	if err != nil {
		return imageError(err)
	}
	if resolution == nil || !digestImage.MatchString(resolution.Reference) {
		return cli.New(cli.ExitUnavailable, "image resolver returned an invalid digest reference")
	}
	o.image = resolution.Reference
	clusterName, clusterNew, err := resolveDeployCluster(cmd.Context(), f.IOStreams, api, o.cluster, createCluster, interactive)
	if err != nil {
		return err
	}
	port := o.port
	if !cmd.Flags().Changed("port") {
		if len(resolution.ExposedPorts) == 1 {
			port = resolution.ExposedPorts[0]
		} else {
			if !interactive {
				return cli.MisuseError("pass --port because the image does not expose exactly one TCP port")
			}
			selected, askErr := cli.PromptLine(cmd.Context(), f.IOStreams, "Port", "8080")
			if askErr != nil {
				return askErr
			}
			n, parseErr := strconv.ParseInt(selected, 10, 32)
			if parseErr != nil {
				return cli.MisuseError("--port must be from 1 to 65535")
			}
			port = int32(n)
		}
	}
	for name, value := range map[string]string{"image": o.image, "cluster": clusterName, "port": strconv.Itoa(int(port))} {
		if err := cmd.Flags().Set(name, value); err != nil {
			return err
		}
	}
	req, err := o.build(cmd, nil, nil, envName)
	if err != nil {
		return err
	}
	format, err := printer.Resolve(f, o.output)
	if err != nil {
		return err
	}
	reviewDeployPlan(f.IOStreams, appName, envName, clusterName, api.Tenant(), appNew, true, clusterNew, nil)
	reviewRelease(f.IOStreams, "Review deploy", appName, envName, source, &client.AppEnvironment{}, req.Web, nil)
	if p.dryRun {
		return nil
	}
	if !interactive && !*yes {
		return cli.MisuseError("refusing to deploy without --yes in non-interactive mode")
	}
	if err := cli.ConfirmAction(f.IOStreams, *yes, "deploy", "Deploy this configuration?"); err != nil {
		return err
	}
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	created := deployCreated{}
	resume := func(err error) error {
		return deployPartialFailure(cmd, f.IOStreams, appName, envName, req.Web.Image, clusterName, o, created, err)
	}
	if clusterNew {
		fmt.Fprintf(f.IOStreams.ErrOut, "creating cluster %s (wait)\n", clusterName)
		var createdCluster *client.Cluster
		createdCluster, err = cluster.CreateAndWait(cmd.Context(), f.IOStreams, api, client.CreateClusterRequest{Name: clusterName, DisplayName: clusterName, Type: "shared"}, true, 30*time.Minute)
		created.Cluster = createdCluster != nil
		if err != nil {
			return resume(err)
		}
	}
	if appNew {
		fmt.Fprintf(f.IOStreams.ErrOut, "creating app %s\n", appName)
		_, _, err = api.Apps().Create(cmd.Context(), client.CreateAppRequest{Name: appName})
		if err != nil {
			return resume(translateErr(err, "", appName))
		}
		created.App = true
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "creating environment %s\n", envName)
	item, _, err := api.AppEnvironments(appName).Create(cmd.Context(), req)
	if err != nil {
		return resume(translateErr(err, appName, envName))
	}
	created.Environment = true
	fmt.Fprintf(f.IOStreams.ErrOut, "deploying %s\n", o.image)
	if item == nil || item.UID == "" || item.Generation <= 0 {
		return resume(cli.New(cli.ExitUnavailable, "environment was created without a verifiable generation; inspect it before retrying"))
	}
	if o.wait {
		item, err = waitForRelease(cmd.Context(), f.IOStreams, api, appName, envName, releaseTarget{uid: item.UID, generation: item.Generation, revision: deploymentRevision(item), web: req.Web, secrets: req.Secrets, notice: deployReleaseNotice(f.IOStreams)}, o.waitTimeout)
		if err != nil {
			return resume(err)
		}
	}
	p.created = created
	return p.render(f, format, appName, envName, item)
}

func existingDeployFlags(cmd *cobra.Command, o *envOptions, e *client.AppEnvironment) error {
	for _, name := range []string{"secret-env", "secret", "domain", "readiness", "cpu", "memory", "cpu-limit", "memory-limit"} {
		if cmd.Flags().Changed(name) {
			return cli.MisuseError("--" + name + " changes an existing environment; use kupe app env update")
		}
	}
	if o.cluster != "" && o.cluster != e.Cluster {
		return cli.MisuseError("an existing environment keeps its cluster; use kupe app env to manage environments")
	}
	return nil
}

func deriveAppName(image string) string {
	image, _, _ = strings.Cut(image, "@")
	image = image[strings.LastIndex(image, "/")+1:]
	image, _, _ = strings.Cut(image, ":")
	var name strings.Builder
	for _, r := range strings.ToLower(image) {
		if r >= 'a' && r <= 'z' || unicode.IsDigit(r) && r < 128 {
			name.WriteRune(r)
		} else if name.Len() > 0 && !strings.HasSuffix(name.String(), "-") {
			name.WriteByte('-')
		}
	}
	value := strings.Trim(name.String(), "-")
	if len(value) > 32 {
		value = strings.TrimRight(value[:32], "-")
	}
	if value == "" {
		return "app"
	}
	return value
}

func resolveDeployCluster(ctx context.Context, streams *cli.IOStreams, api client.Interface, name string, create, interactive bool) (string, bool, error) {
	if name != "" {
		if err := validateName(name, "cluster", 63); err != nil {
			return "", false, err
		}
		_, _, err := api.Clusters().Get(ctx, name)
		if err == nil {
			return name, false, nil
		}
		if !deployMissing(err) {
			return "", false, releaseRequestError(err, "", "", false)
		}
		if !create {
			return "", false, cli.MisuseError("cluster does not exist; pass --create-cluster to create it")
		}
		return name, true, nil
	}
	if create {
		return resolveDeployCluster(ctx, streams, api, "dev", true, interactive)
	}
	items, err := api.Clusters().List(ctx)
	if err != nil {
		return "", false, releaseRequestError(err, "", "", false)
	}
	switch len(items) {
	case 0:
		if !interactive {
			return "", false, cli.MisuseError("no clusters exist; pass --create-cluster to create dev")
		}
		answer, err := cli.PromptLine(ctx, streams, "Create cluster dev? (yes/no)", "no")
		if err != nil {
			return "", false, err
		}
		if !strings.EqualFold(answer, "yes") {
			return "", false, cli.MisuseError("a cluster is required; pass --cluster or --create-cluster")
		}
		return "dev", true, nil
	case 1:
		return items[0].Name, false, nil
	default:
		if !interactive {
			return "", false, cli.MisuseError("pass --cluster to choose a cluster")
		}
		choices := make([]string, len(items))
		for i, item := range items {
			choices[i] = item.Name
		}
		sort.Strings(choices)
		name, err := chooseDeployValue(ctx, streams, "Cluster", choices)
		return name, false, err
	}
}

func chooseDeployValue(ctx context.Context, streams *cli.IOStreams, label string, values []string) (string, error) {
	for i, value := range values {
		fmt.Fprintf(streams.ErrOut, "  %d. %s\n", i+1, value)
	}
	answer, err := cli.PromptLine(ctx, streams, label, "1")
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(values) {
		return "", cli.MisuseError("choose a listed " + strings.ToLower(label))
	}
	return values[n-1], nil
}

func reviewDeployPlan(streams *cli.IOStreams, app, env, clusterName, tenant string, newApp, newEnv, newCluster bool, status *client.AppEnvironmentStatus) {
	state := func(new bool) string {
		if new {
			return "new"
		}
		return "existing"
	}
	fmt.Fprintf(streams.ErrOut, "  app: %s (%s)\n  environment: %s (%s)\n  cluster: %s (%s)\n", app, state(newApp), env, state(newEnv), clusterName, state(newCluster))
	address := "assigned after deployment"
	if tenant != "" {
		address = app + "-" + env + "." + clusterName + "." + tenant + ".clusters (domain assigned by platform)"
	}
	if status != nil && status.Address != "" {
		address = status.Address
	}
	fmt.Fprintln(streams.ErrOut, "  address: "+address)
}

func deployPartialFailure(cmd *cobra.Command, streams *cli.IOStreams, app, env, image, clusterName string, o *envOptions, created deployCreated, err error) error {
	if !created.App && !created.Environment && !created.Cluster {
		return err
	}
	var names []string
	if created.Cluster {
		names = append(names, "cluster "+clusterName)
	}
	if created.App {
		names = append(names, "app "+app)
	}
	if created.Environment {
		names = append(names, "environment "+app+"/"+env)
	}
	fmt.Fprintln(streams.ErrOut, "Created "+strings.Join(names, ", ")+"; these resources were kept.")
	resume := []string{"kupe", "app", "deploy", shellArgument(app), shellArgument(env), "--image", shellArgument(image), "--yes"}
	appendFlag := func(name, value string) { resume = append(resume, "--"+name, shellArgument(value)) }
	// Retain only explicitly supplied non-secret target selectors. Never copy
	// --token or authentication values into a command that may be logged.
	for _, name := range []string{"context", "tenant", "api-url", "config"} {
		if flag := cmd.Flag(name); flag != nil && flag.Changed {
			appendFlag(name, flag.Value.String())
		}
	}
	appendFlag("cluster", clusterName)
	if !created.Environment {
		appendFlag("port", strconv.Itoa(int(o.port)))
		for _, name := range []string{"replicas", "readiness", "cpu", "memory", "cpu-limit", "memory-limit"} {
			if cmd.Flags().Changed(name) {
				appendFlag(name, cmd.Flags().Lookup(name).Value.String())
			}
		}
		for _, pair := range []struct {
			name   string
			values []string
		}{{"secret", o.secrets}, {"secret-env", o.secretEnv}, {"domain", o.domains}} {
			for _, value := range pair.values {
				appendFlag(pair.name, value)
			}
		}
		for i, entry := range o.env {
			name, _, _ := strings.Cut(entry, "=")
			placeholder := fmt.Sprintf("KUPE_DEPLOY_VALUE_%d", i+1)
			// App variable names may contain dots and hyphens; shell parameters
			// need portable generated identifiers. Keep their mapping explicit.
			fmt.Fprintf(streams.ErrOut, "Set %s locally for app variable %s before retrying.\n", placeholder, name)
			resume = append(resume, "--set-env", `"`+name+`=${`+placeholder+`?set `+placeholder+` before retrying}"`)
		}
		if len(o.env) > 0 {
			fmt.Fprintln(streams.ErrOut, "Literal values are omitted from the resume command.")
		}
	}
	if !o.wait {
		resume = append(resume, "--wait=false")
	}
	if cmd.Flags().Changed("wait-timeout") {
		appendFlag("wait-timeout", o.waitTimeout.String())
	}
	if o.output != "" {
		appendFlag("output", o.output)
	}
	fmt.Fprintln(streams.ErrOut, "Resume: "+strings.Join(resume, " "))
	return err
}

func shellArgument(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("_./:@=+-", r)
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func chooseDeployImage(ctx context.Context, streams *cli.IOStreams, api client.Interface) (string, error) {
	registries, err := api.Registries().List(ctx)
	if err != nil && client.ErrorCode(err) != "registries_unavailable" {
		return "", releaseRequestError(err, "", "", false)
	}
	if err == nil && len(registries) > 0 {
		repositories, listErr := registry.AllItems(ctx, func(ctx context.Context, page, size int) (*client.Page[client.Repository], error) {
			return api.ListRepositories(ctx, registries[0].Name, client.ListRepositoriesOptions{Page: page, PageSize: size})
		})
		if listErr != nil {
			return "", releaseRequestError(listErr, "", "", false)
		}
		choices := make([]string, 0, len(repositories)+1)
		for _, repo := range repositories {
			choices = append(choices, repo.Name)
		}
		choices = append(choices, "Type an image reference")
		if len(repositories) > 0 {
			selected, err := chooseDeployValue(ctx, streams, "Image repository", choices)
			if err != nil {
				return "", err
			}
			for _, repo := range repositories {
				if repo.Name != selected {
					continue
				}
				tagged := true
				images, err := registry.AllItems(ctx, func(ctx context.Context, page, size int) (*client.Page[client.Image], error) {
					return api.ListImages(ctx, registries[0].Name, repo.Name, client.ListImagesOptions{Tagged: &tagged, Page: page, PageSize: size})
				})
				if err != nil {
					return "", releaseRequestError(err, "", "", false)
				}
				var references []string
				for _, image := range images {
					for _, tag := range image.Tags {
						references = append(references, repo.PullReference+":"+tag.Name)
					}
				}
				sort.Strings(references)
				if len(references) > 0 {
					return chooseDeployValue(ctx, streams, "Image", references)
				}
			}
		}
	}
	return cli.PromptLine(ctx, streams, "Image", "")
}

func deployReleaseNotice(streams *cli.IOStreams) func(int64) {
	return func(number int64) { fmt.Fprintf(streams.ErrOut, "waiting for release %d\n", number) }
}
