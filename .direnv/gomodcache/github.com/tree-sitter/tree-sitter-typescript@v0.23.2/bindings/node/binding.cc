#include <napi.h>

typedef struct TSLanguage TSLanguage;

extern "C" TSLanguage *tree_sitter_typescript();
extern "C" TSLanguage *tree_sitter_tsx();

// "tree-sitter", "language" hashed with BLAKE2
const napi_type_tag LANGUAGE_TYPE_TAG = {
    0x8AF2E5212AD58ABF, 0xD5006CAD83ABBA16
};

Napi::Object Init(Napi::Env env, Napi::Object exports) {
    auto typescript = Napi::Object::New(env);
    typescript["name"] = Napi::String::New(env, "typescript");
    auto typescript_language = Napi::External<TSLanguage>::New(env, tree_sitter_typescript());
    typescript_language.TypeTag(&LANGUAGE_TYPE_TAG);
    typescript["language"] = typescript_language;

    auto tsx = Napi::Object::New(env);
    tsx["name"] = Napi::String::New(env, "tsx");
    auto tsx_language = Napi::External<TSLanguage>::New(env, tree_sitter_tsx());
    tsx_language.TypeTag(&LANGUAGE_TYPE_TAG);
    tsx["language"] = tsx_language;

    exports["typescript"] = typescript;
    exports["tsx"] = tsx;
    return exports;
}

NODE_API_MODULE(tree_sitter_typescript_binding, Init)
