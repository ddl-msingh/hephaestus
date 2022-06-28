package component

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	"github.com/dominodatalab/controller-util/core"
	"github.com/newrelic/go-agent/v3/newrelic"
	"gomodules.xyz/jsonpatch/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	hephv1 "github.com/dominodatalab/hephaestus/pkg/api/hephaestus/v1"
	"github.com/dominodatalab/hephaestus/pkg/config"
	"github.com/dominodatalab/hephaestus/pkg/messaging/amqp"
)

var publishContentType = "application/json"

type StatusMessengerComponent struct {
	cfg      config.Messaging
	newRelic *newrelic.Application
}

func StatusMessenger(cfg config.Messaging, nr *newrelic.Application) *StatusMessengerComponent {
	return &StatusMessengerComponent{
		cfg:      cfg,
		newRelic: nr,
	}
}

func (c *StatusMessengerComponent) Reconcile(ctx *core.Context) (ctrl.Result, error) {
	log := ctx.Log
	obj := ctx.Object.(*hephv1.ImageBuild)

	txn := c.newRelic.StartTransaction("StatusMessengerComponent.Reconcile")
	txn.AddAttribute("imagebuild", obj.ObjectKey().String())
	txn.AddAttribute("url", c.cfg.AMQP.URL)
	defer txn.End()

	if !c.cfg.Enabled {
		log.V(1).Info("Aborting reconcile, messaging is not enabled")
		txn.Ignore()

		return ctrl.Result{}, nil
	}

	log.Info("Creating AMQP message publisher")
	connectSeg := txn.StartSegment("broker-connect")
	publisher, err := amqp.NewPublisher(ctx.Log, c.cfg.AMQP.URL)
	if err != nil {
		txn.NoticeError(newrelic.Error{
			Message: err.Error(),
			Class:   "BrokerConnectError",
		})
		return ctrl.Result{}, err
	}
	connectSeg.End()

	defer func() {
		log.V(1).Info("Closing message publisher")
		if err := publisher.Close(); err != nil {
			log.Error(err, "Failed to close message publisher")
		}

		log.V(1).Info("Message publisher closed")
	}()

	publishOpts := amqp.PublishOptions{
		ExchangeName: c.cfg.AMQP.Exchange,
		QueueName:    c.cfg.AMQP.Queue,
		ContentType:  publishContentType,
	}

	if ov := obj.Spec.AMQPOverrides; ov != nil {
		if ov.ExchangeName != "" {
			log.Info("Overriding target AMQP Exchange", "name", ov.ExchangeName)
			publishOpts.ExchangeName = ov.ExchangeName
		}

		if ov.QueueName != "" {
			log.Info("Overriding target AMQP Queue", "name", ov.QueueName)
			publishOpts.QueueName = ov.QueueName
		}
	}
	txn.AddAttribute("queue", publishOpts.QueueName)
	txn.AddAttribute("exchange", publishOpts.ExchangeName)

	for idx, transition := range obj.Status.Transitions {
		if transition.Processed {
			log.V(1).Info("Transition has been processed, skipping", "transition", transition)
			continue
		}
		log.Info("Processing phase transition", "from", transition.PreviousPhase, "to", transition.Phase)

		transitionSeg := txn.StartSegment(fmt.Sprintf("transition-to-%s", strings.ToLower(string(transition.Phase))))
		transitionSeg.AddAttribute("previous-phase", string(transition.PreviousPhase))

		log.V(1).Info("Building object link")
		objLink, err := BuildObjectLink(ctx)
		if err != nil {
			txn.NoticeError(newrelic.Error{
				Message: err.Error(),
				Class:   "ObjectLinkError",
			})

			return ctrl.Result{}, err
		}

		occurredAt := time.Now()
		if transition.OccurredAt != nil {
			occurredAt = transition.OccurredAt.Time
		}

		message := hephv1.ImageBuildStatusTransitionMessage{
			Name:          obj.Name,
			Annotations:   obj.Annotations,
			ObjectLink:    objLink,
			PreviousPhase: transition.PreviousPhase,
			CurrentPhase:  transition.Phase,
			OccurredAt:    metav1.Time{Time: occurredAt},
		}

		// return image urls when build succeeds
		if transition.Phase == hephv1.PhaseSucceeded {
			var images []string
			for _, image := range obj.Spec.Images {
				// parse the image name and tag
				named, err := reference.ParseNormalizedNamed(image)
				if err != nil {
					txn.NoticeError(newrelic.Error{
						Message: err.Error(),
						Class:   "ParseImageError",
					})

					return ctrl.Result{}, fmt.Errorf("parsing image name %q failed: %w", image, err)
				}

				// add the latest tag when one is not provided
				named = reference.TagNameOnly(named)
				images = append(images, named.String())
			}
			message.ImageURLs = images
		}

		log.V(1).Info("Marshalling StatusMessage into JSON", "message", message)
		content, err := json.Marshal(message)
		if err != nil {
			txn.NoticeError(newrelic.Error{
				Message: err.Error(),
				Class:   "StatusMessageMarshalError",
			})

			return ctrl.Result{}, err
		}

		publishOpts.Body = content

		log.Info("Publishing transition message")
		if err = publisher.Publish(publishOpts); err != nil {
			txn.NoticeError(newrelic.Error{
				Message: err.Error(),
				Class:   "MessagePublishError",
			})

			return ctrl.Result{}, err
		}

		log.Info("Generating JSON patch for status transition")
		transition.Processed = true
		ops := []jsonpatch.Operation{
			{
				Operation: "replace",
				Path:      fmt.Sprintf("/status/transitions/%d", idx),
				Value:     transition,
			},
		}

		patch, err := json.Marshal(ops)
		if err != nil {
			txn.NoticeError(newrelic.Error{
				Message: err.Error(),
				Class:   "GeneratePatchError",
			})

			return ctrl.Result{}, fmt.Errorf("could not generate transition patch: %w", err)
		}
		log.V(1).Info("Generated JSON", "patch", string(patch))

		log.Info("Patching processed status transition", "phase", transition.Phase)
		if err := ctx.Client.Status().Patch(ctx, obj, client.RawPatch(types.JSONPatchType, patch)); err != nil {
			txn.NoticeError(newrelic.Error{
				Message: err.Error(),
				Class:   "ApplyPatchError",
			})

			return ctrl.Result{}, err
		}

		transitionSeg.End()
	}

	return ctrl.Result{}, nil
}

func BuildObjectLink(ctx *core.Context) (string, error) {
	gvk, err := apiutil.GVKForObject(ctx.Object, ctx.Scheme)
	if err != nil {
		return "", err
	}

	link := path.Join(
		"/apis",
		gvk.GroupVersion().String(),
		"namespaces",
		ctx.Object.GetNamespace(),
		strings.ToLower(gvk.Kind),
		ctx.Object.GetName(),
	)
	return link, nil
}
