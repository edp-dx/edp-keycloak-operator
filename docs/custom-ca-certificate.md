# Custom CA Certificate Usage

This document provides instructions on how to specify a custom CA certificate for the Keycloak operator to use when communicating with Keycloak instances that use self-signed certificates.

## Creating a ConfigMap or Secret with the Custom CA Certificate

1. Create a ConfigMap or Secret that contains your custom CA certificate:

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: my-custom-ca
   data:
     ca.crt: <base64-encoded-certificate>
   ```

   Alternatively, if you are using a ConfigMap:

   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: my-custom-ca
   data:
     ca.crt: <certificate-contents>
   ```

2. Apply the ConfigMap or Secret to your Kubernetes cluster:

   ```sh
   kubectl apply -f my-custom-ca.yaml
   ```

## Updating Keycloak Custom Resources to Use the Custom CA Certificate

Add a reference to the `caCertificate` field in your `Keycloak` or `ClusterKeycloak` resources to specify the custom CA certificate:

```yaml
apiVersion: keycloak.org/v1alpha1
kind: Keycloak
metadata:
  name: example-keycloak
spec:
  caCertificate: my-custom-ca
```

The operator will use the specified CA certificate for secure communication with the Keycloak instance.
