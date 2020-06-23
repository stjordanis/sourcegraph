package resolvers

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/sourcegraph/go-diff/diff"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/db"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend"
	ee "github.com/sourcegraph/sourcegraph/enterprise/internal/campaigns"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/campaigns"
)

const patchSetIDKind = "PatchSet"

func marshalPatchSetID(id int64) graphql.ID {
	return relay.MarshalID(patchSetIDKind, id)
}

func unmarshalPatchSetID(id graphql.ID) (patchSetID int64, err error) {
	err = relay.UnmarshalSpec(id, &patchSetID)
	return
}

const patchIDKind = "Patch"

func marshalPatchID(id int64) graphql.ID {
	return relay.MarshalID(patchIDKind, id)
}

func unmarshalPatchID(id graphql.ID) (cid int64, err error) {
	err = relay.UnmarshalSpec(id, &cid)
	return
}

func patchSetDiffStat(ctx context.Context, store *ee.Store, opts ee.ListPatchesOpts) (*graphqlbackend.DiffStat, error) {
	noDiffOpts := opts
	noDiffOpts.NoDiff = true
	patches, _, err := store.ListPatches(ctx, noDiffOpts)
	if err != nil {
		return nil, err
	}

	repoIDs := make([]api.RepoID, 0, len(patches))
	for _, p := range patches {
		repoIDs = append(repoIDs, p.RepoID)
	}

	// 🚨 SECURITY: We use db.Repos.GetByIDs to filter out repositories the
	// user doesn't have access to.
	accessibleRepos, err := db.Repos.GetByIDs(ctx, repoIDs...)
	if err != nil {
		return nil, err
	}

	accessibleRepoIDs := make(map[api.RepoID]struct{}, len(accessibleRepos))
	for _, r := range accessibleRepos {
		accessibleRepoIDs[r.ID] = struct{}{}
	}

	total := &graphqlbackend.DiffStat{}
	for _, p := range patches {
		// 🚨 SECURITY: We filter out the patches that belong to repositories the
		// user does NOT have access to.
		if _, ok := accessibleRepoIDs[p.RepoID]; !ok {
			continue
		}

		s, ok := p.DiffStat()
		if !ok {
			return nil, fmt.Errorf("patch %d has no diff stat", p.ID)
		}

		total.AddStat(s)
	}

	return total, nil
}

func fileDiffConnectionCompute(patch *campaigns.Patch) func(ctx context.Context, args *graphqlbackend.FileDiffsConnectionArgs) ([]*diff.FileDiff, int32, bool, error) {
	var (
		once        sync.Once
		fileDiffs   []*diff.FileDiff
		afterIdx    int32
		hasNextPage bool
		err         error
	)
	return func(ctx context.Context, args *graphqlbackend.FileDiffsConnectionArgs) ([]*diff.FileDiff, int32, bool, error) {
		once.Do(func() {
			if args.After != nil {
				parsedIdx, err := strconv.ParseInt(*args.After, 0, 32)
				if err != nil {
					return
				}
				if parsedIdx < 0 {
					parsedIdx = 0
				}
				afterIdx = int32(parsedIdx)
			}
			totalAmount := afterIdx
			if args.First != nil {
				totalAmount += *args.First
			}

			dr := diff.NewMultiFileDiffReader(strings.NewReader(patch.Diff))
			for {
				var fileDiff *diff.FileDiff
				fileDiff, err = dr.ReadFile()
				if err == io.EOF {
					err = nil
					break
				}
				if err != nil {
					return
				}
				fileDiffs = append(fileDiffs, fileDiff)
				if len(fileDiffs) == int(totalAmount) {
					// Check for hasNextPage.
					_, err = dr.ReadFile()
					if err != nil && err != io.EOF {
						return
					}
					if err == io.EOF {
						err = nil
					} else {
						hasNextPage = true
					}
					break
				}
			}
		})
		return fileDiffs, afterIdx, hasNextPage, err
	}
}

func previewNewFile(r *graphqlbackend.FileDiffResolver) graphqlbackend.FileResolver {
	fileStat := graphqlbackend.CreateFileInfo(r.FileDiff.NewName, false)
	return graphqlbackend.NewVirtualFileResolver(fileStat, fileDiffVirtualFileContent(r))
}

func fileDiffVirtualFileContent(r *graphqlbackend.FileDiffResolver) graphqlbackend.FileContentFunc {
	var (
		once       sync.Once
		newContent string
		err        error
	)
	return func(ctx context.Context) (string, error) {
		once.Do(func() {
			var oldContent string
			if oldFile := r.OldFile(); oldFile != nil {
				var err error
				oldContent, err = r.OldFile().Content(ctx)
				if err != nil {
					return
				}
			}
			newContent = applyPatch(oldContent, r.FileDiff)
		})
		return newContent, err
	}
}

func applyPatch(fileContent string, fileDiff *diff.FileDiff) string {
	contentLines := strings.Split(fileContent, "\n")
	newContentLines := make([]string, 0)
	var lastLine int32 = 1
	// Assumes the hunks are sorted by ascending lines.
	for _, hunk := range fileDiff.Hunks {
		// Detect holes.
		if hunk.OrigStartLine != 0 && hunk.OrigStartLine != lastLine {
			originalLines := contentLines[lastLine-1 : hunk.OrigStartLine-1]
			newContentLines = append(newContentLines, originalLines...)
			lastLine += int32(len(originalLines))
		}
		hunkLines := strings.Split(string(hunk.Body), "\n")
		for _, line := range hunkLines {
			switch {
			case line == "":
				// Skip
			case strings.HasPrefix(line, "-"):
				lastLine++
			case strings.HasPrefix(line, "+"):
				newContentLines = append(newContentLines, line[1:])
			default:
				newContentLines = append(newContentLines, contentLines[lastLine-1])
				lastLine++
			}
		}
	}
	// Append remaining lines from original file.
	if origLines := int32(len(contentLines)); origLines > 0 && origLines != lastLine {
		newContentLines = append(newContentLines, contentLines[lastLine-1:]...)
	}
	return strings.Join(newContentLines, "\n")
}
