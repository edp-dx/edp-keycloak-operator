# Custom CA Certificate Usage in Keycloak Operator

This document explains how to configure the Keycloak Operator to use a custom CA certificate for communicating with Keycloak instances that use self-signed certificates.

## Creating a ConfigMap or Secret

1. Create a `ConfigMap` or `Secret` containing your custom CA certificate:

   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: my-custom-ca
   data:
     ca.crt: |
       -----BEGIN CERTIFICATE-----
       ...your certificate here...
       -----END CERTIFICATE-----
   ```

   Or use a `Secret` if you prefer to keep the certificate data encrypted in etcd:

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: my-custom-ca
   type: Opaque
   data:
     ca.crt: ...base64 encoded cert...
   ```

## Updating Keycloak Custom Resource

To reference the custom CA certificate in your Keycloak or ClusterKeycloak resource, add the `caCertificate` field under `spec`:

   ```yaml
   apiVersion: keycloak.org/v1alpha1
   kind: Keycloak
   metadata:
     name: keycloak
   spec:
     caCertificate:
       configMap: my-custom-ca
   ```

Replace `configMap` with `secret` if you're using a Secret.

## Operator Behavior

When the Keycloak Operator reconciles a Keycloak or ClusterKeycloak resource, it will check for the `caCertificate` field. If found, it will retrieve the CA certificate from the specified ConfigMap or Secret and use it to configure the gocloak client, ensuring secure communication with the Keycloak instance.