# Registry credential mount

This directory is only a local Compose mount point. Do not commit credentials.

- Bearer mode: create an ignored `bearer-token` file and set `REGISTRY_BEARER_TOKEN_FILE=/var/run/secrets/cloudops/registry/bearer-token`.
- Basic mode: create ignored `username` and `password` files and set the corresponding `REGISTRY_USERNAME_FILE` and `REGISTRY_PASSWORD_FILE` container paths.

Production deployments should use a pre-existing Kubernetes Secret through the Helm registry auth values.
