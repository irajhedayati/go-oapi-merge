package merge_test

import (
    "fmt"
    "os"

    "github.com/irajhedayati/go-oapi-merge/merge"
)

func ExampleOapiYaml() {
    _ = os.WriteFile("api.yaml", []byte(`openapi: "3.0.0"
  info: {title: Demo, version: "1.0"}
  paths: {}
  `), 0644)

    if err := merge.OapiYaml("api.yaml", "merged.yaml"); err != nil {
        fmt.Println(err)
    }
    // Output:
    // (nothing)
}
