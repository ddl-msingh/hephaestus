// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/dominodatalab/hephaestus/pkg/api/hephaestus/v1"
	"github.com/dominodatalab/hephaestus/pkg/clientset/scheme"
	rest "k8s.io/client-go/rest"
)

type HephaestusV1Interface interface {
	RESTClient() rest.Interface
	ImageBuildsGetter
	ImageCachesGetter
}

// HephaestusV1Client is used to interact with features provided by the hephaestus group.
type HephaestusV1Client struct {
	restClient rest.Interface
}

func (c *HephaestusV1Client) ImageBuilds(namespace string) ImageBuildInterface {
	return newImageBuilds(c, namespace)
}

func (c *HephaestusV1Client) ImageCaches(namespace string) ImageCacheInterface {
	return newImageCaches(c, namespace)
}

// NewForConfig creates a new HephaestusV1Client for the given config.
func NewForConfig(c *rest.Config) (*HephaestusV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &HephaestusV1Client{client}, nil
}

// NewForConfigOrDie creates a new HephaestusV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *HephaestusV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new HephaestusV1Client for the given RESTClient.
func New(c rest.Interface) *HephaestusV1Client {
	return &HephaestusV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *HephaestusV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
