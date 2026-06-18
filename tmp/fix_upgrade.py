import sys

with open('../fix_upgrade.py.log', 'w') as log:
    sys.stderr = log
    sys.stdout = log
    
    with open('internal/cli/upgrade.go', 'r', encoding='utf-8') as f:
        content = f.read()

    old1 = 'type ghRelease struct {\n\tTagName string `json:"tag_name"`\n\tAssets  []ghAsset\n}'
    new1 = 'type ghRelease struct {\n\tTagName string    `json:"tag_name"`\n\tBody    string    `json:"body"`\n\tAssets  []ghAsset\n}'
    content = content.replace(old1, new1)

    old2 = '\tif *checkOnly {\n\t\treturn 0\n\t}\n\n\t// 5. Find'
    new2 = '\tif *checkOnly {\n\t\tif rel.Body != "" {\n\t\t\tfmt.Printf("\\n%s\\n", strings.TrimSpace(rel.Body))\n\t\t}\n\t\treturn 0\n\t}\n\n\t// 5. Find'
    content = content.replace(old2, new2)

    with open('internal/cli/upgrade.go', 'w', encoding='utf-8') as f:
        f.write(content)
    print('done')
