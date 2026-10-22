package printer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// Registry renders the tenant's registry.
var Registry = Resource[client.Registry]{
	Columns:       registryColumns,
	DetailColumns: registryDetailColumns,
	Name:          func(r *client.Registry) string { return r.Name },
}

// RegistryBinding renders cluster connections. The detail view ends with a
// table of the push namespaces.
var RegistryBinding = registryBindingResource{Resource[client.RegistryBinding]{
	Columns:       registryBindingColumns,
	DetailColumns: registryBindingDetailColumns,
	Name:          func(b *client.RegistryBinding) string { return b.Name },
}}

// Repository renders repositories.
var Repository = Resource[client.Repository]{
	Columns:       repositoryColumns,
	DetailColumns: repositoryDetailColumns,
	Name:          func(r *client.Repository) string { return r.Name },
}

// Image renders images; -o name prints REPOSITORY@DIGEST.
var Image = Resource[client.Image]{
	Columns:       imageColumns,
	DetailColumns: imageDetailColumns,
	Name:          func(i *client.Image) string { return i.Repository + "@" + i.Digest },
}

// RegistryToken renders access tokens and login sessions. Never a secret:
// list and get responses carry none.
var RegistryToken = Resource[client.RegistryToken]{
	Columns:       registryTokenColumns,
	DetailColumns: registryTokenDetailColumns,
	Name:          func(t *client.RegistryToken) string { return t.ID },
}

func registryColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return registry(v).Name }},
		{Name: "PHASE", Get: func(v any) string { return ux.ColorPhase(registryStatus(registry(v)).Phase, colorEnabled) }},
		{Name: "ENDPOINT", Get: func(v any) string { return registryStatus(registry(v)).Endpoint }},
		{Name: "STORAGE", Get: func(v any) string { return registryStorage(registry(v)) }},
		{Name: "REPOSITORIES", Get: func(v any) string { return strconv.FormatInt(registryStatus(registry(v)).RepositoryCount, 10) }},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(registry(v).CreatedAt) }},
		{Name: "PROTECTED", Wide: true, Get: func(v any) string { return onOff(registry(v).DeletionProtection) }},
		{Name: "LIMIT", Wide: true, Get: func(v any) string { return registryLimitSetting(registry(v)) }},
	}
}

func registryDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return registry(v).Name }},
		{Name: "Phase", Get: func(v any) string { return ux.ColorPhase(registryStatus(registry(v)).Phase, colorEnabled) }},
		{Name: "Endpoint", Get: func(v any) string { return registryStatus(registry(v)).Endpoint }},
		{Name: "Storage", Get: func(v any) string { return registryStorage(registry(v)) }},
		{Name: "Storage Limit", Get: func(v any) string { return registryLimitSetting(registry(v)) }},
		{Name: "Repositories", Get: func(v any) string {
			return strconv.FormatInt(registryStatus(registry(v)).RepositoryCount, 10)
		}},
		{Name: "Deletion Protection", Get: func(v any) string { return onOff(registry(v).DeletionProtection) }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(registry(v).CreatedAt) }},
	}
}

// registryStorage is "1.2 GiB of 5.0 GiB": what the registry stores and the
// cap in force ("unlimited", or only the usage before a cap is applied).
func registryStorage(r *client.Registry) string {
	st := registryStatus(r)
	used := HumanBytes(st.StorageUsedBytes)
	switch {
	case st.StorageLimitBytes == nil:
		return used
	case *st.StorageLimitBytes < 0:
		return used + " of unlimited"
	}
	return used + " of " + HumanBytes(*st.StorageLimitBytes)
}

// registryLimitSetting is the tenant's own limit, or "plan" when the plan's
// cap applies.
func registryLimitSetting(r *client.Registry) string {
	if r.StorageLimit == "" {
		return "plan"
	}
	return r.StorageLimit
}

// HumanBytes renders a byte count in binary units with one decimal: "512 B",
// "1.2 GiB".
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// ShortDigest is "sha256:" and the digest's first 12 hex characters.
func ShortDigest(digest string) string {
	algo, hex, ok := strings.Cut(digest, ":")
	if !ok || len(hex) <= 12 {
		return digest
	}
	return algo + ":" + hex[:12]
}

// registryBindingResource is a Resource whose detail view lists the push
// namespaces below the fields.
type registryBindingResource struct {
	Resource[client.RegistryBinding]
}

// One writes one connection; a table view ends with its push namespaces.
func (r registryBindingResource) One(io *cli.IOStreams, format *Format, item *client.RegistryBinding) error {
	if format.Kind != Table && format.Kind != Wide {
		return r.Resource.One(io, format, item)
	}
	if err := PrintDetails(io.Out, item, r.DetailColumns(io.ColorEnabled)); err != nil {
		return err
	}
	rows := pushNamespaceRows(item)
	if len(rows) == 0 {
		return nil
	}
	fmt.Fprintln(io.Out)
	return PrintTable(io.Out, rows, pushNamespaceColumns(io.ColorEnabled), true)
}

// pushNamespaceRow is one push namespace of a connection.
type pushNamespaceRow struct {
	name, phase, message string
}

// pushNamespaceRows lists the push namespaces in order, then any the
// platform still reports that are no longer listed (Removing).
func pushNamespaceRows(b *client.RegistryBinding) []pushNamespaceRow {
	var reported []client.RegistryBindingNamespaceStatus
	if b.Status != nil {
		reported = b.Status.Namespaces
	}
	rows := make([]pushNamespaceRow, 0, len(b.PushNamespaces))
	listed := map[string]bool{}
	for _, ns := range b.PushNamespaces {
		listed[ns] = true
		row := pushNamespaceRow{name: ns, phase: client.PhasePending}
		for _, st := range reported {
			if st.Name == ns && st.Phase != "" {
				row.phase, row.message = st.Phase, st.Message
			}
		}
		rows = append(rows, row)
	}
	for _, st := range reported {
		if !listed[st.Name] {
			rows = append(rows, pushNamespaceRow{name: st.Name, phase: phaseRemoving, message: st.Message})
		}
	}
	return rows
}

func pushNamespaceColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "PUSH-NAMESPACE", Get: func(v any) string { return rowOf[pushNamespaceRow](v).name }},
		{Name: "PHASE", Get: func(v any) string { return ux.ColorPhase(rowOf[pushNamespaceRow](v).phase, colorEnabled) }},
		{Name: "MESSAGE", Get: func(v any) string { return rowOf[pushNamespaceRow](v).message }},
	}
}

func registryBindingColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "CLUSTER", Get: func(v any) string { return registryBinding(v).Cluster }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(registryBindingStatus(registryBinding(v)).Phase, colorEnabled)
		}},
		{Name: "PUSH-NAMESPACES", Get: func(v any) string { return listOrNone(registryBinding(v).PushNamespaces) }},
		{Name: "PULL-USERNAME", Get: func(v any) string { return registryBindingStatus(registryBinding(v)).PullUsername }},
		{Name: "ROTATION", Get: func(v any) string { return rotationDisplay(registryBinding(v)) }},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(registryBinding(v).CreatedAt) }},
		{Name: "PUSH-SECRET", Wide: true, Get: func(v any) string { return registryBinding(v).SecretName }},
		{Name: "PUSH-USERNAME", Wide: true, Get: func(v any) string { return registryBindingStatus(registryBinding(v)).PushUsername }},
		{Name: "MESSAGE", Wide: true, Get: func(v any) string { return registryBindingStatus(registryBinding(v)).Message }},
	}
}

func registryBindingDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Cluster", Get: func(v any) string { return registryBinding(v).Cluster }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(registryBindingStatus(registryBinding(v)).Phase, colorEnabled)
		}},
		{Name: "Message", Get: func(v any) string { return registryBindingStatus(registryBinding(v)).Message }},
		{Name: "Pull Username", Get: func(v any) string { return registryBindingStatus(registryBinding(v)).PullUsername }},
		{Name: "Push Namespaces", Get: func(v any) string { return listOrNone(registryBinding(v).PushNamespaces) }},
		{Name: "Push Secret", Get: func(v any) string { return registryBinding(v).SecretName }},
		{Name: "Push Username", Get: func(v any) string { return registryBindingStatus(registryBinding(v)).PushUsername }},
		{Name: "Rotation", Get: func(v any) string { return rotationDisplay(registryBinding(v)) }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(registryBinding(v).CreatedAt) }},
	}
}

// rotationDisplay is "pending" while a credential rotation is being
// applied, else "none".
func rotationDisplay(b *client.RegistryBinding) string {
	if rot := registryBindingStatus(b).Rotation; rot != nil && rot.Pending {
		return "pending"
	}
	return "none"
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func repositoryColumns(bool) Columns {
	return Columns{
		{Name: "REPOSITORY", Get: func(v any) string { return repository(v).Name }},
		{Name: "IMAGES", Get: func(v any) string { return strconv.FormatInt(repository(v).ImageCount, 10) }},
		{Name: "PULLS", Get: func(v any) string { return strconv.FormatInt(repository(v).PullCount, 10) }},
		{Name: "UPDATED", Get: func(v any) string { return ageFromRFC3339(repository(v).UpdatedAt) }},
		{Name: "PULL-REFERENCE", Wide: true, Get: func(v any) string { return repository(v).PullReference }},
		{Name: "CREATED", Wide: true, Get: func(v any) string { return ageFromRFC3339(repository(v).CreatedAt) }},
	}
}

func repositoryDetailColumns(bool) Columns {
	return Columns{
		{Name: "Repository", Get: func(v any) string { return repository(v).Name }},
		{Name: "Pull Reference", Get: func(v any) string { return repository(v).PullReference }},
		{Name: "Images", Get: func(v any) string { return strconv.FormatInt(repository(v).ImageCount, 10) }},
		{Name: "Pulls", Get: func(v any) string { return strconv.FormatInt(repository(v).PullCount, 10) }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(repository(v).CreatedAt) }},
		{Name: "Updated", Get: func(v any) string { return createdWithAge(repository(v).UpdatedAt) }},
	}
}

func imageColumns(bool) Columns {
	return Columns{
		{Name: "DIGEST", Get: func(v any) string { return ShortDigest(image(v).Digest) }},
		{Name: "TAGS", Get: func(v any) string { return imageTags(image(v)) }},
		{Name: "SIZE", Get: func(v any) string { return HumanBytes(image(v).Size) }},
		{Name: "PLATFORMS", Get: func(v any) string { return imagePlatforms(image(v)) }},
		{Name: "PUSHED", Get: func(v any) string { return ageFromRFC3339(image(v).PushedAt) }},
		{Name: "TYPE", Wide: true, Get: func(v any) string { return image(v).Type }},
		{Name: "LAST-PULLED", Wide: true, Get: func(v any) string { return agesOrNever(image(v).LastPulledAt) }},
		{Name: "FULL-DIGEST", Wide: true, Get: func(v any) string { return image(v).Digest }},
	}
}

func imageDetailColumns(bool) Columns {
	return Columns{
		{Name: "Repository", Get: func(v any) string { return image(v).Repository }},
		{Name: "Digest", Get: func(v any) string { return image(v).Digest }},
		{Name: "Type", Get: func(v any) string { return image(v).Type }},
		{Name: "Tags", Get: func(v any) string { return imageTags(image(v)) }},
		{Name: "Size", Get: func(v any) string { return HumanBytes(image(v).Size) }},
		{Name: "Platforms", Get: func(v any) string { return imagePlatforms(image(v)) }},
		{Name: "Pushed", Get: func(v any) string { return createdWithAge(image(v).PushedAt) }},
		{Name: "Last Pulled", Get: func(v any) string { return agesOrNever(image(v).LastPulledAt) }},
		{Name: "Pull By Digest", Get: func(v any) string { return image(v).Pull.ByDigest }},
		{Name: "Pull By Tag", Get: func(v any) string { return listOrNone(image(v).Pull.ByTag) }},
	}
}

// imageTags is the tags comma-joined, or "untagged".
func imageTags(img *client.Image) string {
	if len(img.Tags) == 0 {
		return "untagged"
	}
	names := make([]string, len(img.Tags))
	for i, t := range img.Tags {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// imagePlatforms is "linux/amd64, linux/arm64/v8", empty when unknown.
func imagePlatforms(img *client.Image) string {
	out := make([]string, 0, len(img.Platforms))
	for _, p := range img.Platforms {
		parts := []string{p.OS, p.Architecture}
		if p.Variant != "" {
			parts = append(parts, p.Variant)
		}
		out = append(out, strings.Join(parts, "/"))
	}
	return strings.Join(out, ", ")
}

func registryTokenColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "ID", Get: func(v any) string { return registryToken(v).ID }},
		{Name: "NAME", Get: func(v any) string { return registryToken(v).DisplayName }},
		{Name: "KIND", Get: func(v any) string { return registryToken(v).Kind }},
		{Name: "ACCESS", Get: func(v any) string { return registryToken(v).Access }},
		{Name: "CREATED-BY", Get: func(v any) string { return registryToken(v).CreatedBy }},
		{Name: "EXPIRES", Get: func(v any) string { return agesOrNever(registryToken(v).ExpiresAt) }},
		{Name: "LAST-USED", Get: func(v any) string { return agesOrNever(registryToken(v).LastUsedAt) }},
		{Name: "PHASE", Wide: true, Get: func(v any) string { return ux.ColorPhase(registryToken(v).Phase(), colorEnabled) }},
		{Name: "USERNAME", Wide: true, Get: func(v any) string { return registryToken(v).Username }},
		{Name: "AGE", Wide: true, Get: func(v any) string { return ageFromRFC3339(registryToken(v).CreatedAt) }},
	}
}

func registryTokenDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "ID", Get: func(v any) string { return registryToken(v).ID }},
		{Name: "Name", Get: func(v any) string { return registryToken(v).DisplayName }},
		{Name: "Kind", Get: func(v any) string { return registryToken(v).Kind }},
		{Name: "Access", Get: func(v any) string { return registryToken(v).Access }},
		{Name: "Username", Get: func(v any) string { return registryToken(v).Username }},
		{Name: "Phase", Get: func(v any) string { return ux.ColorPhase(registryToken(v).Phase(), colorEnabled) }},
		{Name: "Message", Get: func(v any) string { return valueOf(registryToken(v).Status).Message }},
		{Name: "Created By", Get: func(v any) string { return registryToken(v).CreatedBy }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(registryToken(v).CreatedAt) }},
		{Name: "Expires", Get: func(v any) string { return agesOrNever(registryToken(v).ExpiresAt) }},
		{Name: "Last Used", Get: func(v any) string { return agesOrNever(registryToken(v).LastUsedAt) }},
	}
}

func registry(v any) *client.Registry { return rowOf[client.Registry](v) }

func registryStatus(r *client.Registry) client.RegistryStatus { return valueOf(r.Status) }

func registryBinding(v any) *client.RegistryBinding { return rowOf[client.RegistryBinding](v) }

func registryBindingStatus(b *client.RegistryBinding) client.RegistryBindingStatus {
	return valueOf(b.Status)
}

func repository(v any) *client.Repository { return rowOf[client.Repository](v) }

func image(v any) *client.Image { return rowOf[client.Image](v) }

func registryToken(v any) *client.RegistryToken { return rowOf[client.RegistryToken](v) }
