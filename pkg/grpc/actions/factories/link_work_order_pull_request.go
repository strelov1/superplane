package factories

import (
	"context"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/database"
	"github.com/superplanehq/superplane/pkg/grpc/actions/messages"
	grpcerrors "github.com/superplanehq/superplane/pkg/grpc/errors"
	"github.com/superplanehq/superplane/pkg/models"
	factoryevents "github.com/superplanehq/superplane/pkg/models/factory"
	"gorm.io/gorm"
)

// LinkWorkOrderPullRequest attaches an open pull request that was opened outside
// SuperPlane (by hand, by another agent) to an existing task, so PR feedback,
// checks and PR Closure treat it like a pull request the Implement line opened.
func LinkWorkOrderPullRequest(
	ctx context.Context,
	deps IntakeDependencies,
	organizationID, factoryID, orderID, pullRequestURL string,
) (*models.FactoryPullRequest, error) {
	orgID, err := parseOrganizationID(organizationID)
	if err != nil {
		return nil, factoryErrorToStatus(err, "failed to link pull request")
	}
	db := database.DB(ctx)
	factory, err := findFactory(db, orgID, factoryID)
	if err != nil {
		return nil, factoryErrorToStatus(err, "failed to link pull request")
	}
	order, err := findWorkOrder(db, factory, orderID)
	if err != nil {
		return nil, factoryErrorToStatus(err, "failed to link pull request")
	}
	pr, err := loadHandOffPullRequest(ctx, db, deps, factory, strings.TrimSpace(pullRequestURL))
	if err != nil {
		return nil, factoryErrorToStatus(err, "failed to link pull request")
	}

	taskRepo := ""
	if order.Repository != nil {
		taskRepo = strings.TrimSpace(*order.Repository)
	}
	if taskRepo != "" && !strings.EqualFold(pr.Repository, taskRepo) {
		return nil, invalidArgument("The pull request repository does not match the task repository.")
	}
	// Only open PRs: PR Closure reacts to the merge webhook, which a merged PR will not send again.
	if !strings.EqualFold(strings.TrimSpace(pr.State), models.FactoryPullRequestStateOpen) {
		return nil, grpcerrors.FailedPrecondition(errFactoryPullRequestNotOpen, "The pull request is not open.")
	}
	headRef := strings.TrimPrefix(strings.TrimSpace(pr.HeadBranch), "refs/heads/")
	headRepo := strings.TrimSpace(pr.HeadRepository)
	if headRepo == "" {
		headRepo = pr.Repository
	}

	var linked *models.FactoryPullRequest
	err = db.Transaction(func(tx *gorm.DB) error {
		created, err := order.CreatePullRequest(tx, models.FactoryPullRequestParams{
			Provider:   pr.Provider,
			ExternalID: pr.ExternalID,
			Repository: pr.Repository,
			Number:     pr.Number,
			URL:        pr.URL,
			Title:      pr.Title,
			State:      models.FactoryPullRequestStateOpen,
		})
		if err != nil {
			return err
		}
		linked = created
		if headRef == "" {
			return nil
		}
		// PR feedback fixes push to the task's branch artifact; add it unless the task already has it.
		var existing int64
		if err := tx.Model(&models.FactoryWorkOrderArtifact{}).
			Where("work_order_id = ? AND type = ? AND data->>'name' = ?", order.ID, models.FactoryWorkOrderArtifactTypeBranch, headRef).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		_, err = order.CreateArtifact(tx, models.FactoryWorkOrderArtifactParams{
			Type: models.FactoryWorkOrderArtifactTypeBranch,
			Data: map[string]any{"name": headRef, "repository": headRepo},
		})
		return err
	})
	if err != nil {
		return nil, factoryErrorToStatus(err, "failed to link pull request")
	}

	// Same follow-up as CreateFactoryPullRequest.
	if err := messages.PublishFactoryWorkOrderUpdated(
		factory.ID.String(),
		order.ID.String(),
		factoryevents.EventTypeOrderPullRequestAdded,
	); err != nil {
		log.WithError(err).Warnf("Failed to publish factory work order updated for order %s", order.ID)
	}
	if linked.Provider == models.FactoryPullRequestProviderGitHub {
		ScheduleFactoryPullRequestMergeabilityRefresh(ctx, deps, factory.OrganizationID, factory.ID, linked.ID)
		StartPullRequestChecks(ctx, deps, linked.ID)
		StartVisualEvidenceCapture(ctx, deps, factory.OrganizationID, factory.ID, linked.ID)
	}
	if err := StartAutomaticCustomAutomations(ctx, deps, factory, order); err != nil {
		log.WithError(err).Warnf("Failed to start custom automation for order %s", order.ID)
	}
	return linked, nil
}
