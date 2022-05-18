apiVersion: kahu.io/v1beta1
kind: Backup
metadata:
   name: backup-demo6
spec:
   includedNamespaces: ["pod5"]
   excludedNamespaces: ["pod7"]
