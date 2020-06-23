package graphqlbackend

import (
	"context"
	"errors"

	graphql "github.com/graph-gophers/graphql-go"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend/externallink"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend/graphqlutil"
	"github.com/sourcegraph/sourcegraph/internal/campaigns"
)

type CreateCampaignArgs struct {
	Namespace    graphql.ID
	CampaignSpec graphql.ID
}

type ApplyCampaignArgs struct {
	Namespace    graphql.ID
	CampaignSpec graphql.ID
	Campaign     *graphql.ID
}

type MoveCampaignArgs struct {
	Campaign     graphql.ID
	NewName      *string
	NewNamespace *graphql.ID
}

type ListCampaignArgs struct {
	First               *int32
	State               *string
	ViewerCanAdminister *bool
}

type CloseCampaignArgs struct {
	Campaign        graphql.ID
	CloseChangesets bool
}

type DeleteCampaignArgs struct {
	Campaign graphql.ID
}

type SyncChangesetArgs struct {
	Changeset graphql.ID
}

type FileDiffsConnectionArgs struct {
	First *int32
	After *string
}

type CampaignSpecInput struct {
	CampaignSpec   string
	ChangesetSpecs []graphql.ID
}

type ComputeCampaignDeltaArgs struct {
	Campaign       graphql.ID
	CampaignSpec   CampaignSpecInput
	CampaignSpecID graphql.ID
}

type CreateChangesetSpecArgs struct {
	ChangesetSpec string
}

type CreateCampaignSpecArgs struct {
	Input *CampaignSpecInput
}

type CampaignsResolver interface {
	// Mutations
	CreateCampaign(ctx context.Context, args *CreateCampaignArgs) (CampaignResolver, error)
	ApplyCampaign(ctx context.Context, args *ApplyCampaignArgs) (CampaignResolver, error)
	MoveCampaign(ctx context.Context, args *MoveCampaignArgs) (CampaignResolver, error)
	CloseCampaign(ctx context.Context, args *CloseCampaignArgs) (CampaignResolver, error)
	DeleteCampaign(ctx context.Context, args *DeleteCampaignArgs) (*EmptyResponse, error)
	CreateChangesetSpec(ctx context.Context, args *CreateChangesetSpecArgs) (ChangesetSpecResolver, error)
	CreateCampaignSpec(ctx context.Context, args *CreateCampaignSpecArgs) (CampaignSpecResolver, error)
	ComputeCampaignDelta(ctx context.Context, args *ComputeCampaignDeltaArgs) (CampaignDeltaResolver, error)
	SyncChangeset(ctx context.Context, args *SyncChangesetArgs) (*EmptyResponse, error)

	// Queries
	Campaigns(ctx context.Context, args *ListCampaignArgs) (CampaignsConnectionResolver, error)
	CampaignByID(ctx context.Context, id graphql.ID) (CampaignResolver, error)
	ChangesetByID(ctx context.Context, id graphql.ID) (ChangesetResolver, error)
}

type CampaignSpecResolver interface {
	ID() (graphql.ID, error)
	ChangesetSpecs() ([]ChangesetSpecResolver, error)
	ExpiresAt() *DateTime
}

type ChangesetSpecResolver interface {
	ID() (graphql.ID, error)
	// TODO: More fields, see PR
	ExpiresAt() *DateTime
}

type CampaignDeltaResolver interface {
	ID() (graphql.ID, error)
	// TODO: More fields, see PR
	CreatedAt() DateTime
}

type ChangesetCountsArgs struct {
	From *DateTime
	To   *DateTime
}

type ListChangesetsArgs struct {
	First       *int32
	State       *campaigns.ChangesetState
	ReviewState *campaigns.ChangesetReviewState
	CheckState  *campaigns.ChangesetCheckState
}

type CampaignResolver interface {
	ID() graphql.ID
	Name() string
	Description() *string
	Branch() *string
	Author(ctx context.Context) (*UserResolver, error)
	ViewerCanAdminister(ctx context.Context) (bool, error)
	URL(ctx context.Context) (string, error)
	Namespace(ctx context.Context) (n NamespaceResolver, err error)
	CreatedAt() DateTime
	UpdatedAt() DateTime
	Changesets(ctx context.Context, args *ListChangesetsArgs) (ChangesetsConnectionResolver, error)
	OpenChangesets(ctx context.Context) (ChangesetsConnectionResolver, error)
	ChangesetCountsOverTime(ctx context.Context, args *ChangesetCountsArgs) ([]ChangesetCountsResolver, error)
	RepositoryDiffs(ctx context.Context, args *graphqlutil.ConnectionArgs) (RepositoryComparisonConnectionResolver, error)
	Status(context.Context) (BackgroundProcessStatus, error)
	ClosedAt() *DateTime
	DiffStat(ctx context.Context) (*DiffStat, error)
}

type CampaignsConnectionResolver interface {
	Nodes(ctx context.Context) ([]CampaignResolver, error)
	TotalCount(ctx context.Context) (int32, error)
	PageInfo(ctx context.Context) (*graphqlutil.PageInfo, error)
}

type ChangesetsConnectionResolver interface {
	Nodes(ctx context.Context) ([]ChangesetResolver, error)
	TotalCount(ctx context.Context) (int32, error)
	PageInfo(ctx context.Context) (*graphqlutil.PageInfo, error)
}

type ChangesetLabelResolver interface {
	Text() string
	Color() string
	Description() *string
}

// ChangesetResolver is the "interface Changeset" in the GraphQL schema and is
// implemented by ExternalChangesetResolver and HiddenExternalChangesetResolver.
type ChangesetResolver interface {
	ID() graphql.ID

	CreatedAt() DateTime
	UpdatedAt() DateTime
	NextSyncAt(ctx context.Context) (*DateTime, error)
	State() campaigns.ChangesetState
	Campaigns(ctx context.Context, args *ListCampaignArgs) (CampaignsConnectionResolver, error)

	ToExternalChangeset() (ExternalChangesetResolver, bool)
	ToHiddenExternalChangeset() (HiddenExternalChangesetResolver, bool)
}

// HiddenExternalChangesetResolver implements only the common interface,
// ChangesetResolver, to not reveal information to unauthorized users.
//
// Theoretically this type is not necessary, but it's easier to understand the
// implementation of the GraphQL schema if we have a mapping between GraphQL
// types and Go types.
type HiddenExternalChangesetResolver interface {
	ChangesetResolver
}

// ExternalChangesetResolver implements the ChangesetResolver interface and
// additional data.
type ExternalChangesetResolver interface {
	ChangesetResolver

	ExternalID() string
	Title() (string, error)
	Body() (string, error)
	ExternalURL() (*externallink.Resolver, error)
	ReviewState(context.Context) campaigns.ChangesetReviewState
	CheckState(context.Context) (*campaigns.ChangesetCheckState, error)
	Repository(ctx context.Context) (*RepositoryResolver, error)

	Events(ctx context.Context, args *struct{ graphqlutil.ConnectionArgs }) (ChangesetEventsConnectionResolver, error)
	Diff(ctx context.Context) (*RepositoryComparisonResolver, error)
	Head(ctx context.Context) (*GitRefResolver, error)
	Base(ctx context.Context) (*GitRefResolver, error)
	Labels(ctx context.Context) ([]ChangesetLabelResolver, error)
}

type ChangesetEventsConnectionResolver interface {
	Nodes(ctx context.Context) ([]ChangesetEventResolver, error)
	TotalCount(ctx context.Context) (int32, error)
	PageInfo(ctx context.Context) (*graphqlutil.PageInfo, error)
}

type ChangesetEventResolver interface {
	ID() graphql.ID
	Changeset(ctx context.Context) (ExternalChangesetResolver, error)
	CreatedAt() DateTime
}

type ChangesetCountsResolver interface {
	Date() DateTime
	Total() int32
	Merged() int32
	Closed() int32
	Open() int32
	OpenApproved() int32
	OpenChangesRequested() int32
	OpenPending() int32
}

type BackgroundProcessStatus interface {
	CompletedCount() int32
	PendingCount() int32

	State() campaigns.BackgroundProcessState

	Errors() []string
}

var campaignsOnlyInEnterprise = errors.New("campaigns and changesets are only available in enterprise")

type defaultCampaignsResolver struct{}

var DefaultCampaignsResolver CampaignsResolver = defaultCampaignsResolver{}

// Mutations
func (defaultCampaignsResolver) CreateCampaign(ctx context.Context, args *CreateCampaignArgs) (CampaignResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) ApplyCampaign(ctx context.Context, args *ApplyCampaignArgs) (CampaignResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) CreateChangesetSpec(ctx context.Context, args *CreateChangesetSpecArgs) (ChangesetSpecResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) CreateCampaignSpec(ctx context.Context, args *CreateCampaignSpecArgs) (CampaignSpecResolver, error) {
	return nil, campaignsOnlyInEnterprise
}
func (defaultCampaignsResolver) ComputeCampaignDelta(ctx context.Context, args *ComputeCampaignDeltaArgs) (CampaignDeltaResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) MoveCampaign(ctx context.Context, args *MoveCampaignArgs) (CampaignResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) CloseCampaign(ctx context.Context, args *CloseCampaignArgs) (CampaignResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) SyncChangeset(ctx context.Context, args *SyncChangesetArgs) (*EmptyResponse, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) DeleteCampaign(ctx context.Context, args *DeleteCampaignArgs) (*EmptyResponse, error) {
	return nil, campaignsOnlyInEnterprise
}

// Queries
func (defaultCampaignsResolver) CampaignByID(ctx context.Context, id graphql.ID) (CampaignResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) Campaigns(ctx context.Context, args *ListCampaignArgs) (CampaignsConnectionResolver, error) {
	return nil, campaignsOnlyInEnterprise
}

func (defaultCampaignsResolver) ChangesetByID(ctx context.Context, id graphql.ID) (ChangesetResolver, error) {
	return nil, campaignsOnlyInEnterprise
}
