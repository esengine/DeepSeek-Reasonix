{
  "targets": [
    {
      "target_name": "tree_sitter_typescript_binding",
      "dependencies": [
        "<!(node -p \"require('node-addon-api').targets\"):node_addon_api_except",
      ],
      "include_dirs": [
        "typescript/src",
      ],
      "sources": [
        "typescript/src/parser.c",
        "typescript/src/scanner.c",
        "tsx/src/parser.c",
        "tsx/src/scanner.c",
        "bindings/node/binding.cc",
      ],
      "conditions": [
        ["OS!='win'", {
          "cflags_c": [
            "-std=c11",
          ],
        }, { # OS == "win"
          "cflags_c": [
            "/std:c11",
            "/utf-8",
          ],
        }],
      ],
    }
  ]
}
