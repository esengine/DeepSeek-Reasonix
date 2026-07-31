#include <napi.h>

typedef struct TSLanguage TSLanguage;

extern "C" TSLanguage *tree_sitter_php();
extern "C" TSLanguage *tree_sitter_php_only();

// "tree-sitter", "language" hashed with BLAKE2
const napi_type_tag LANGUAGE_TYPE_TAG = {
    0x8AF2E5212AD58ABF, 0xD5006CAD83ABBA16
};

Napi::Object Init(Napi::Env env, Napi::Object exports) {
    auto php = Napi::Object::New(env);
    php["name"] = Napi::String::New(env, "php");
    auto php_language = Napi::External<TSLanguage>::New(env, tree_sitter_php());
    php_language.TypeTag(&LANGUAGE_TYPE_TAG);
    php["language"] = php_language;

    auto php_only = Napi::Object::New(env);
    php_only["name"] = Napi::String::New(env, "php_only");
    auto php_only_language = Napi::External<TSLanguage>::New(env, tree_sitter_php_only());
    php_only_language.TypeTag(&LANGUAGE_TYPE_TAG);
    php_only["language"] = php_only_language;

    exports["php"] = php;
    exports["php_only"] = php_only;
    return exports;
}

NODE_API_MODULE(tree_sitter_php_binding, Init)
