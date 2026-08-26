Fetch Go and cache modules.
Zero dependencies.

```yaml
steps:
  - uses: actions/checkout@v4
  
  - uses: ndx-technologies/gh-setup-go@v1
    id: setup-go
    with:
      go-version: '1.27.0'
  
  - run: go build ./... && go test ./... 
  
  # optional
  - name: save go mod cache
    run: gh-setup-go -save-cache -key "${{ steps.setup-go.outputs.key }}"
```
