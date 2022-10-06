variable "project_id" {
  type        = string
  description = "The project ID in which to host the GKE cluster."
}

variable "region" {
  type        = string
  description = "The region in which the cluster is hosted."
}

variable "kubernetes_version" {
  type        = string
  description = "The Kubernetes version of the masters. If set to 'latest' it will pull latest available version in the selected region."
  default     = "latest"
}
