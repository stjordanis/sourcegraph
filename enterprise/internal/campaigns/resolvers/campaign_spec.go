package resolvers

import (
	"time"

	"github.com/graph-gophers/graphql-go"
	"github.com/pkg/errors"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend"
)

var _ graphqlbackend.CampaignSpecResolver = &campaignSpecResolver{}

type campaignSpecResolver struct {
}

func (r *campaignSpecResolver) ID() (graphql.ID, error) {
	return "", errors.New("TODO: not implemented")
}

func (r *campaignSpecResolver) ChangesetSpecs() ([]graphqlbackend.ChangesetSpecResolver, error) {
	return []graphqlbackend.ChangesetSpecResolver{}, errors.New("TODO: not implemented")
}

func (r *campaignSpecResolver) ExpiresAt() *graphqlbackend.DateTime {
	return &graphqlbackend.DateTime{Time: time.Now()}
}
