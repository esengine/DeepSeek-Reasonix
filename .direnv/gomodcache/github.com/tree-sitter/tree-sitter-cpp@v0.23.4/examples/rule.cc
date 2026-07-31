#include "compiler/rule.h"
#include "compiler/util/hash_combine.h"

namespace tree_sitter {
namespace rules {

using std::move;
using std::vector;
using util::hash_combine;

Rule::Rule(const Rule &other) : blank_(Blank{}), type(BlankType) {
  *this = other;
}

Rule::Rule(Rule &&other) noexcept : blank_(Blank{}), type(BlankType) {
  *this = move(other);
}

static void destroy_value(Rule *rule) {
  switch (rule->type) {
    case Rule::BlankType: return rule->blank_.~Blank();
    case Rule::CharacterSetType: return rule->character_set_.~CharacterSet();
    case Rule::StringType: return rule->string_ .~String();
    case Rule::PatternType: return rule->pattern_ .~Pattern();
    case Rule::NamedSymbolType: return rule->named_symbol_.~NamedSymbol();
    case Rule::SymbolType: return rule->symbol_ .~Symbol();
    case Rule::ChoiceType: return rule->choice_ .~Choice();
    case Rule::MetadataType: return rule->metadata_ .~Metadata();
    case Rule::RepeatType: return rule->repeat_ .~Repeat();
    case Rule::SeqType: return rule->seq_ .~Seq();
  }
}

Rule &Rule::operator=(const Rule &other) {
  destroy_value(this);
  type = other.type;
  switch (type) {
    case BlankType:
      new (&blank_) Blank(other.blank_);
      break;
    case CharacterSetType:
      new (&character_set_) CharacterSet(other.character_set_);
      break;
    case StringType:
      new (&string_) String(other.string_);
      break;
    case PatternType:
      new (&pattern_) Pattern(other.pattern_);
      break;
    case NamedSymbolType:
      new (&named_symbol_) NamedSymbol(other.named_symbol_);
      break;
    case SymbolType:
      new (&symbol_) Symbol(other.symbol_);
      break;
    case ChoiceType:
      new (&choice_) Choice(other.choice_);
      break;
    case MetadataType:
      new (&metadata_) Metadata(other.metadata_);
      break;
    case RepeatType:
      new (&repeat_) Repeat(other.repeat_);
      break;
    case SeqType:
      new (&seq_) Seq(other.seq_);
      break;
  }
  return *this;
}

Rule &Rule::operator=(Rule &&other) noexcept {
  destroy_value(this);
  type = other.type;
  switch (type) {
    case BlankType:
      new (&blank_) Blank(move(other.blank_));
      break;
    case CharacterSetType:
      new (&character_set_) CharacterSet(move(other.character_set_));
      break;
    case StringType:
      new (&string_) String(move(other.string_));
      break;
    case PatternType:
      new (&pattern_) Pattern(move(other.pattern_));
      break;
    case NamedSymbolType:
      new (&named_symbol_) NamedSymbol(move(other.named_symbol_));
      break;
    case SymbolType:
      new (&symbol_) Symbol(move(other.symbol_));
      break;
    case ChoiceType:
      new (&choice_) Choice(move(other.choice_));
      break;
    case MetadataType:
      new (&metadata_) Metadata(move(other.metadata_));
      break;
    case RepeatType:
      new (&repeat_) Repeat(move(other.repeat_));
      break;
    case SeqType:
      new (&seq_) Seq(move(other.seq_));
      break;
  }
  other.type = BlankType;
  other.blank_ = Blank{};
  return *this;
}

Rule::~Rule() noexcept {
  destroy_value(this);
}

bool Rule::operator==(const Rule &other) const {
  if (type != other.type) return false;
  switch (type) {
    case Rule::CharacterSetType: return character_set_ == other.character_set_;
    case Rule::StringType: return string_ == other.string_;
    case Rule::PatternType: return pattern_ == other.pattern_;
    case Rule::NamedSymbolType: return named_symbol_ == other.named_symbol_;
    case Rule::SymbolType: return symbol_ == other.symbol_;
    case Rule::ChoiceType: return choice_ == other.choice_;
    case Rule::MetadataType: return metadata_ == other.metadata_;
    case Rule::RepeatType: return repeat_ == other.repeat_;
    case Rule::SeqType: return seq_ == other.seq_;
    default: return blank_ == other.blank_;
  }
}

template <>
bool Rule::is<Blank>() const { return type == BlankType; }

template <>
bool Rule::is<Symbol>() const { return type == SymbolType; }

template <>
bool Rule::is<Repeat>() const { return type == RepeatType; }

template <>
const Symbol & Rule::get_unchecked<Symbol>() const { return symbol_; }

static inline void add_choice_element(std::vector<Rule> *elements, const Rule &new_rule) {
  new_rule.match(
    [elements](Choice choice) {
      for (auto &element : choice.elements) {
        add_choice_element(elements, element);
      }
    },

    [elements](auto rule) {
      for (auto &element : *elements) {
        if (element == rule) return;
      }
      elements->push_back(rule);
    }
  );
}

Rule Rule::choice(const vector<Rule> &rules) {
  vector<Rule> elements;
  for (auto &element : rules) {
    add_choice_element(&elements, element);
  }
  return (elements.size() == 1) ? elements.front() : Choice{elements};
}

Rule Rule::repeat(const Rule &rule) {
  return rule.is<Repeat>() ? rule : Repeat{rule};
}

Rule Rule::seq(const vector<Rule> &rules) {
  Rule result;
  for (const auto &rule : rules) {
    rule.match(
      [](Blank) {},
      [&](Metadata metadata) {
        if (!metadata.rule->is<Blank>()) {
          result = Seq{result, rule};
        }
      },
      [&](auto) {
        if (result.is<Blank>()) {
          result = rule;
        } else {
          result = Seq{result, rule};
        }
      }
    );
  }
  return result;
}

}  // namespace rules
}  // namespace tree_sitter

namespace std {

size_t hash<Symbol>::operator()(const Symbol &symbol) const {
  auto result = hash<int>()(symbol.index);
  hash_combine(&result, hash<int>()(symbol.type));
  return result;
}

size_t hash<NamedSymbol>::operator()(const NamedSymbol &symbol) const {
  return hash<string>()(symbol.value);
}

size_t hash<Pattern>::operator()(const Pattern &symbol) const {
  return hash<string>()(symbol.value);
}

size_t hash<String>::operator()(const String &symbol) const {
  return hash<string>()(symbol.value);
}

size_t hash<CharacterSet>::operator()(const CharacterSet &character_set) const {
  size_t result = 0;
  hash_combine(&result, character_set.includes_all);
  hash_combine(&result, character_set.included_chars.size());
  for (uint32_t c : character_set.included_chars) {
    hash_combine(&result, c);
  }
  hash_combine(&result, character_set.excluded_chars.size());
  for (uint32_t c : character_set.excluded_chars) {
    hash_combine(&result, c);
  }
  return result;
}

size_t hash<Blank>::operator()(const Blank &blank) const {
  return 0;
}

size_t hash<Choice>::operator()(const Choice &choice) const {
  size_t result = 0;
  for (const auto &element : choice.elements) {
    symmetric_hash_combine(&result, element);
  }
  return result;
}

size_t hash<Repeat>::operator()(const Repeat &repeat) const {
  size_t result = 0;
  hash_combine(&result, *repeat.rule);
  return result;
}

size_t hash<Seq>::operator()(const Seq &seq) const {
  size_t result = 0;
  hash_combine(&result, *seq.left);
  hash_combine(&result, *seq.right);
  return result;
}

size_t hash<Metadata>::operator()(const Metadata &metadata) const {
  size_t result = 0;
  hash_combine(&result, *metadata.rule);
  hash_combine(&result, metadata.params.precedence);
  hash_combine<int>(&result, metadata.params.associativity);
  hash_combine(&result, metadata.params.has_precedence);
  hash_combine(&result, metadata.params.has_associativity);
  hash_combine(&result, metadata.params.is_token);
  hash_combine(&result, metadata.params.is_string);
  hash_combine(&result, metadata.params.is_active);
  hash_combine(&result, metadata.params.is_main_token);
  return result;
}

size_t hash<Rule>::operator()(const Rule &rule) const {
  size_t result = hash<int>()(rule.type);
  switch (rule.type) {
    case Rule::CharacterSetType: return result ^ hash<CharacterSet>()(rule.character_set_);
    case Rule::StringType: return result ^ hash<String>()(rule.string_);
    case Rule::PatternType: return result ^ hash<Pattern>()(rule.pattern_);
    case Rule::NamedSymbolType: return result ^ hash<NamedSymbol>()(rule.named_symbol_);
    case Rule::SymbolType: return result ^ hash<Symbol>()(rule.symbol_);
    case Rule::ChoiceType: return result ^ hash<Choice>()(rule.choice_);
    case Rule::MetadataType: return result ^ hash<Metadata>()(rule.metadata_);
    case Rule::RepeatType: return result ^ hash<Repeat>()(rule.repeat_);
    case Rule::SeqType: return result ^ hash<Seq>()(rule.seq_);
    default: return result ^ hash<Blank>()(rule.blank_);
  }
}

}  // namespace std