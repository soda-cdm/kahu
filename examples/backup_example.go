apiVersion: kahu.io/v1beta1
kind: Backup
metadata:
   name: backup-demo-11
spec:
   includedNamespaces: ["pod1"]
   excludedNamespaces: ["pod2"]
