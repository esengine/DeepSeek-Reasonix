#pragma once

#include <cstdint>
#include <string>
#include <utility>
#include <vector>

// Json is the one value type the protocol needs: requests are parsed into it
// and every reply is built from it. Objects keep insertion order.
class Json {
public:
    enum class Kind { Null, Bool, Number, String, Array, Object };

    Json() = default;
    Json(bool b) : kind_(Kind::Bool), b_(b) {}
    Json(int n) : kind_(Kind::Number), n_(n) {}
    Json(unsigned n) : kind_(Kind::Number), n_(n) {}
    Json(long n) : kind_(Kind::Number), n_(n) {}
    Json(unsigned long n) : kind_(Kind::Number), n_(n) {}
    Json(long long n) : kind_(Kind::Number), n_(static_cast<double>(n)) {}
    Json(unsigned long long n) : kind_(Kind::Number), n_(static_cast<double>(n)) {}
    Json(double n) : kind_(Kind::Number), n_(n) {}
    Json(const char* s) : kind_(Kind::String), s_(s) {}
    Json(std::string s) : kind_(Kind::String), s_(std::move(s)) {}

    static Json array() { Json j; j.kind_ = Kind::Array; return j; }
    static Json object() { Json j; j.kind_ = Kind::Object; return j; }

    Kind kind() const { return kind_; }
    bool isNumber() const { return kind_ == Kind::Number; }
    bool isString() const { return kind_ == Kind::String; }
    bool isObject() const { return kind_ == Kind::Object; }
    double number() const { return n_; }
    const std::string& string() const { return s_; }
    const std::vector<Json>& items() const { return a_; }

    Json& push(Json v);
    Json& set(const std::string& key, Json v);
    const Json* get(const std::string& key) const;

    std::string dump() const;
    // parse reads one JSON text; false on anything malformed or trailing.
    static bool parse(const std::string& text, Json& out);

private:
    void write(std::string& out) const;

    Kind kind_ = Kind::Null;
    bool b_ = false;
    double n_ = 0;
    std::string s_;
    std::vector<Json> a_;
    std::vector<std::pair<std::string, Json>> o_;
};

// quote renders s as a JSON string literal.
std::string quote(const std::string& s);
