resources:
  - ${operator_default_config}
images:
  - name: livewyer/tamoss-operator
    newName: ${repository}
    newTag: ${tag}
