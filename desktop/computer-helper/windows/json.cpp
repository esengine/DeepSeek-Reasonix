#include "json.h"

#include <cmath>
#include <cstdio>
#include <cstdlib>

Json& Json::push(Json v) {
    a_.push_back(std::move(v));
    return *this;
}

Json& Json::set(const std::string& key, Json v) {
    for (auto& [k, existing] : o_) {
        if (k == key) {
            existing = std::move(v);
            return *this;
        }
    }
    o_.emplace_back(key, std::move(v));
    return *this;
}

const Json* Json::get(const std::string& key) const {
    for (const auto& [k, v] : o_) {
        if (k == key) return &v;
    }
    return nullptr;
}

std::string quote(const std::string& s) {
    std::string out = "\"";
    for (unsigned char c : s) {
        switch (c) {
        case '"': out += "\\\""; break;
        case '\\': out += "\\\\"; break;
        case '\n': out += "\\n"; break;
        case '\r': out += "\\r"; break;
        case '\t': out += "\\t"; break;
        default:
            if (c < 0x20) {
                char buf[8];
                std::snprintf(buf, sizeof buf, "\\u%04x", c);
                out += buf;
            } else {
                out += static_cast<char>(c);
            }
        }
    }
    return out + "\"";
}

void Json::write(std::string& out) const {
    switch (kind_) {
    case Kind::Null: out += "null"; break;
    case Kind::Bool: out += b_ ? "true" : "false"; break;
    case Kind::Number: {
        char buf[32];
        if (!std::isfinite(n_)) {
            out += "0";
        } else if (n_ == std::floor(n_) && std::fabs(n_) < 1e15) {
            std::snprintf(buf, sizeof buf, "%.0f", n_);
            out += buf;
        } else {
            std::snprintf(buf, sizeof buf, "%.17g", n_);
            out += buf;
        }
        break;
    }
    case Kind::String: out += quote(s_); break;
    case Kind::Array:
        out += '[';
        for (size_t i = 0; i < a_.size(); i++) {
            if (i) out += ',';
            a_[i].write(out);
        }
        out += ']';
        break;
    case Kind::Object:
        out += '{';
        for (size_t i = 0; i < o_.size(); i++) {
            if (i) out += ',';
            out += quote(o_[i].first);
            out += ':';
            o_[i].second.write(out);
        }
        out += '}';
        break;
    }
}

std::string Json::dump() const {
    std::string out;
    write(out);
    return out;
}

namespace {

struct Parser {
    const std::string& t;
    size_t i = 0;
    int depth = 0;

    void space() {
        while (i < t.size() && (t[i] == ' ' || t[i] == '\t' || t[i] == '\n' || t[i] == '\r')) i++;
    }

    bool literal(const char* word) {
        size_t n = std::char_traits<char>::length(word);
        if (t.compare(i, n, word) != 0) return false;
        i += n;
        return true;
    }

    static void utf8(std::string& out, uint32_t cp) {
        if (cp < 0x80) {
            out += static_cast<char>(cp);
        } else if (cp < 0x800) {
            out += static_cast<char>(0xC0 | (cp >> 6));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        } else if (cp < 0x10000) {
            out += static_cast<char>(0xE0 | (cp >> 12));
            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        } else {
            out += static_cast<char>(0xF0 | (cp >> 18));
            out += static_cast<char>(0x80 | ((cp >> 12) & 0x3F));
            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        }
    }

    bool hex4(uint32_t& v) {
        if (i + 4 > t.size()) return false;
        v = 0;
        for (int k = 0; k < 4; k++) {
            char c = t[i++];
            v <<= 4;
            if (c >= '0' && c <= '9') v |= c - '0';
            else if (c >= 'a' && c <= 'f') v |= c - 'a' + 10;
            else if (c >= 'A' && c <= 'F') v |= c - 'A' + 10;
            else return false;
        }
        return true;
    }

    bool str(std::string& out) {
        if (i >= t.size() || t[i] != '"') return false;
        i++;
        while (i < t.size()) {
            char c = t[i++];
            if (c == '"') return true;
            if (static_cast<unsigned char>(c) < 0x20) return false;
            if (c != '\\') {
                out += c;
                continue;
            }
            if (i >= t.size()) return false;
            switch (t[i++]) {
            case '"': out += '"'; break;
            case '\\': out += '\\'; break;
            case '/': out += '/'; break;
            case 'b': out += '\b'; break;
            case 'f': out += '\f'; break;
            case 'n': out += '\n'; break;
            case 'r': out += '\r'; break;
            case 't': out += '\t'; break;
            case 'u': {
                uint32_t cp;
                if (!hex4(cp)) return false;
                if (cp >= 0xD800 && cp < 0xDC00 && t.compare(i, 2, "\\u") == 0) {
                    size_t back = i;
                    i += 2;
                    uint32_t low;
                    if (hex4(low) && low >= 0xDC00 && low < 0xE000) {
                        cp = 0x10000 + ((cp - 0xD800) << 10) + (low - 0xDC00);
                    } else {
                        i = back;
                        cp = 0xFFFD;
                    }
                } else if (cp >= 0xD800 && cp < 0xE000) {
                    cp = 0xFFFD;
                }
                utf8(out, cp);
                break;
            }
            default: return false;
            }
        }
        return false;
    }

    bool value(Json& out) {
        if (++depth > 64) return false;
        space();
        if (i >= t.size()) return false;
        char c = t[i];
        bool ok = true;
        if (c == '{') {
            i++;
            out = Json::object();
            space();
            if (i < t.size() && t[i] == '}') {
                i++;
            } else {
                for (;;) {
                    space();
                    std::string key;
                    Json v;
                    if (!str(key)) { ok = false; break; }
                    space();
                    if (i >= t.size() || t[i++] != ':') { ok = false; break; }
                    if (!value(v)) { ok = false; break; }
                    out.set(key, std::move(v));
                    space();
                    if (i < t.size() && t[i] == ',') { i++; continue; }
                    if (i < t.size() && t[i] == '}') { i++; break; }
                    ok = false;
                    break;
                }
            }
        } else if (c == '[') {
            i++;
            out = Json::array();
            space();
            if (i < t.size() && t[i] == ']') {
                i++;
            } else {
                for (;;) {
                    Json v;
                    if (!value(v)) { ok = false; break; }
                    out.push(std::move(v));
                    space();
                    if (i < t.size() && t[i] == ',') { i++; continue; }
                    if (i < t.size() && t[i] == ']') { i++; break; }
                    ok = false;
                    break;
                }
            }
        } else if (c == '"') {
            std::string s;
            ok = str(s);
            out = Json(std::move(s));
        } else if (literal("true")) {
            out = Json(true);
        } else if (literal("false")) {
            out = Json(false);
        } else if (literal("null")) {
            out = Json();
        } else {
            const char* start = t.c_str() + i;
            char* end = nullptr;
            double n = std::strtod(start, &end);
            ok = end != start;
            i += end - start;
            out = Json(n);
        }
        depth--;
        return ok;
    }
};

} // namespace

bool Json::parse(const std::string& text, Json& out) {
    Parser p{text};
    if (!p.value(out)) return false;
    p.space();
    return p.i == text.size();
}
