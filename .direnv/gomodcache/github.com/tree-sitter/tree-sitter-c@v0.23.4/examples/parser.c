#include "runtime/parser.h"
#include <assert.h>
#include <stdio.h>
#include <limits.h>
#include <stdbool.h>
#include "tree_sitter/runtime.h"
#include "runtime/tree.h"
#include "runtime/lexer.h"
#include "runtime/length.h"
#include "runtime/array.h"
#include "runtime/language.h"
#include "runtime/alloc.h"
#include "runtime/reduce_action.h"
#include "runtime/error_costs.h"

#define LOG(...)                                                           \
  if (self->lexer.logger.log) {                                            \
    snprintf(self->lexer.debug_buffer, TS_DEBUG_BUFFER_SIZE, __VA_ARGS__); \
    self->lexer.logger.log(self->lexer.logger.payload, TSLogTypeParse,     \
                           self->lexer.debug_buffer);                      \
  }                                                                        \
  if (self->print_debugging_graphs) {                                      \
    fprintf(stderr, "graph {\nlabel=\"");                                  \
    fprintf(stderr, __VA_ARGS__);                                          \
    fprintf(stderr, "\"\n}\n\n");                                          \
  }

#define LOG_STACK()                                                     \
  if (self->print_debugging_graphs) {                                   \
    ts_stack_print_dot_graph(self->stack, self->language->symbol_names, \
                             stderr);                                   \
    fputs("\n\n", stderr);                                              \
  }

#define LOG_TREE()                                                        \
  if (self->print_debugging_graphs) {                                     \
    ts_tree_print_dot_graph(self->finished_tree, self->language, stderr); \
    fputs("\n", stderr);                                                  \
  }

#define SYM_NAME(symbol) ts_language_symbol_name(self->language, symbol)

typedef struct {
  Parser *parser;
  TSSymbol lookahead_symbol;
  TreeArray *trees_above_error;
  uint32_t tree_count_above_error;
  bool found_repair;
  ReduceAction best_repair;
  TSStateId best_repair_next_state;
  uint32_t best_repair_skip_count;
} ErrorRepairSession;

typedef struct {
  Parser *parser;
  TSSymbol lookahead_symbol;
} SkipPrecedingTreesSession;

static void parser__push(Parser *self, StackVersion version, Tree *tree,
                         TSStateId state) {
  ts_stack_push(self->stack, version, tree, false, state);
  ts_tree_release(tree);
}

static bool parser__breakdown_top_of_stack(Parser *self, StackVersion version) {
  bool did_break_down = false;
  bool pending = false;

  do {
    StackPopResult pop = ts_stack_pop_pending(self->stack, version);
    if (!pop.slices.size)
      break;

    did_break_down = true;
    pending = false;
    for (uint32_t i = 0; i < pop.slices.size; i++) {
      StackSlice slice = pop.slices.contents[i];
      TSStateId state = ts_stack_top_state(self->stack, slice.version);
      Tree *parent = *array_front(&slice.trees);

      for (uint32_t j = 0; j < parent->child_count; j++) {
        Tree *child = parent->children[j];
        pending = child->child_count > 0;

        if (child->symbol == ts_builtin_sym_error) {
          state = ERROR_STATE;
        } else if (!child->extra) {
          state = ts_language_next_state(self->language, state, child->symbol);
        }

        ts_stack_push(self->stack, slice.version, child, pending, state);
      }

      for (uint32_t j = 1; j < slice.trees.size; j++) {
        Tree *tree = slice.trees.contents[j];
        parser__push(self, slice.version, tree, state);
      }

      LOG("breakdown_top_of_stack tree:%s", SYM_NAME(parent->symbol));
      LOG_STACK();

      ts_stack_decrease_push_count(self->stack, slice.version,
                                   parent->child_count + 1);
      ts_tree_release(parent);
      array_delete(&slice.trees);
    }
  } while (pending);

  return did_break_down;
}

static bool parser__breakdown_lookahead(Parser *self, Tree **lookahead,
                                        TSStateId state,
                                        ReusableNode *reusable_node) {
  bool result = false;
  while (reusable_node->tree->child_count > 0 &&
         (self->is_split || reusable_node->tree->parse_state != state ||
          reusable_node->tree->fragile_left ||
          reusable_node->tree->fragile_right)) {
    LOG("state_mismatch sym:%s", SYM_NAME(reusable_node->tree->symbol));
    reusable_node_breakdown(reusable_node);
    result = true;
  }

  if (result) {
    ts_tree_release(*lookahead);
    ts_tree_retain(*lookahead = reusable_node->tree);
  }

  return result;
}

static inline bool ts_lex_mode_eq(TSLexMode self, TSLexMode other) {
  return self.lex_state == other.lex_state &&
    self.external_lex_state == other.external_lex_state;
}

static bool parser__can_reuse(Parser *self, TSStateId state, Tree *tree,
                              TableEntry *table_entry) {
  TSLexMode current_lex_mode = self->language->lex_modes[state];
  if (ts_lex_mode_eq(tree->first_leaf.lex_mode, current_lex_mode))
    return true;
  if (current_lex_mode.external_lex_state != 0)
    return false;
  if (tree->size.bytes == 0)
    return false;
  if (!table_entry->is_reusable)
    return false;
  if (!table_entry->depends_on_lookahead)
    return true;
  return tree->child_count > 1 && tree->error_cost == 0;
}

typedef int CondenseResult;
static int CondenseResultMadeChange = 1;
static int CondenseResultAllVersionsHadError = 2;

static CondenseResult parser__condense_stack(Parser *self) {
  CondenseResult result = 0;
  bool has_version_without_errors = false;

  for (StackVersion i = 0; i < ts_stack_version_count(self->stack); i++) {
    if (ts_stack_is_halted(self->stack, i)) {
      ts_stack_remove_version(self->stack, i);
      result |= CondenseResultMadeChange;
      i--;
      continue;
    }

    ErrorStatus error_status = ts_stack_error_status(self->stack, i);
    if (error_status.count == 0) has_version_without_errors = true;

    for (StackVersion j = 0; j < i; j++) {
      if (ts_stack_merge(self->stack, j, i)) {
        result |= CondenseResultMadeChange;
        i--;
        break;
      }

      switch (error_status_compare(error_status,
                                   ts_stack_error_status(self->stack, j))) {
        case -1:
          ts_stack_remove_version(self->stack, j);
          result |= CondenseResultMadeChange;
          i--;
          j--;
          break;
        case 1:
          ts_stack_remove_version(self->stack, i);
          result |= CondenseResultMadeChange;
          i--;
          break;
      }
    }
  }

  if (!has_version_without_errors && ts_stack_version_count(self->stack) > 0) {
    result |= CondenseResultAllVersionsHadError;
  }

  return result;
}

static void parser__restore_external_scanner(Parser *self, StackVersion version) {
  const TSExternalTokenState *state = ts_stack_external_token_state(self->stack, version);
  if (self->lexer.last_external_token_state != state) {
    LOG("restore_external_scanner");
    self->lexer.last_external_token_state = state;
    if (state) {
      self->language->external_scanner.deserialize(
        self->external_scanner_payload,
        *state
      );
    } else {
      self->language->external_scanner.reset(self->external_scanner_payload);
    }
  }
}

static Tree *parser__lex(Parser *self, StackVersion version) {
  TSStateId parse_state = ts_stack_top_state(self->stack, version);
  Length start_position = ts_stack_top_position(self->stack, version);
  TSLexMode lex_mode = self->language->lex_modes[parse_state];
  const bool *valid_external_tokens = ts_language_enabled_external_tokens(
    self->language,
    lex_mode.external_lex_state
  );

  bool found_external_token = false;
  bool found_error = false;
  bool skipped_error = false;
  int32_t first_error_character = 0;
  Length error_start_position, error_end_position;
  ts_lexer_reset(&self->lexer, start_position);

  for (;;) {
    Length current_position = self->lexer.current_position;

    if (valid_external_tokens) {
      LOG("lex_external state:%d, row:%u, column:%u", lex_mode.external_lex_state,
          current_position.extent.row, current_position.extent.column);
      parser__restore_external_scanner(self, version);
      ts_lexer_start(&self->lexer);
      if (self->language->external_scanner.scan(self->external_scanner_payload,
                                                &self->lexer.data, valid_external_tokens)) {
        if (length_has_unknown_chars(self->lexer.token_end_position)) {
          self->lexer.token_end_position = self->lexer.current_position;
        }
        if (lex_mode.lex_state != 0 ||
            self->lexer.token_end_position.bytes > current_position.bytes) {
          found_external_token = true;
          break;
        }
      }
      ts_lexer_reset(&self->lexer, current_position);
    }

    LOG("lex_internal state:%d, row:%u, column:%u", lex_mode.lex_state,
        current_position.extent.row, current_position.extent.column);
    ts_lexer_start(&self->lexer);
    if (self->language->lex_fn(&self->lexer.data, lex_mode.lex_state)) {
      if (length_has_unknown_chars(self->lexer.token_end_position)) {
        self->lexer.token_end_position = self->lexer.current_position;
      }
      break;
    }

    if (!found_error) {
      LOG("retry_in_error_mode");
      found_error = true;
      lex_mode = self->language->lex_modes[ERROR_STATE];
      valid_external_tokens = ts_language_enabled_external_tokens(
        self->language,
        lex_mode.external_lex_state
      );
      ts_lexer_reset(&self->lexer, start_position);
      continue;
    }

    if (!skipped_error) {
      LOG("skip_unrecognized_character");
      skipped_error = true;
      error_start_position = self->lexer.token_start_position;
      error_end_position = self->lexer.token_start_position;
      first_error_character = self->lexer.data.lookahead;
    }

    if (self->lexer.current_position.bytes == error_end_position.bytes) {
      if (self->lexer.data.lookahead == 0) {
        self->lexer.data.result_symbol = ts_builtin_sym_error;
        break;
      }
      self->lexer.data.advance(&self->lexer, false);
    }

    error_end_position = self->lexer.current_position;
  }

  Tree *result;
  if (skipped_error) {
    Length padding = length_sub(error_start_position, start_position);
    Length size = length_sub(error_end_position, error_start_position);
    result = ts_tree_make_error(size, padding, first_error_character);
  } else {
    TSSymbol symbol = self->lexer.data.result_symbol;
    if (found_external_token) {
      symbol = self->language->external_scanner.symbol_map[symbol];
    }

    Length padding = length_sub(self->lexer.token_start_position, start_position);
    Length size = length_sub(self->lexer.token_end_position, self->lexer.token_start_position);
    TSSymbolMetadata metadata = ts_language_symbol_metadata(self->language, symbol);
    result = ts_tree_make_leaf(symbol, padding, size, metadata);

    if (found_external_token) {
      result->has_external_tokens = true;
      result->has_external_token_state = true;
      memset(result->external_token_state, 0, sizeof(TSExternalTokenState));
      self->language->external_scanner.serialize(self->external_scanner_payload, result->external_token_state);
      self->lexer.last_external_token_state = &result->external_token_state;
    }
  }

  result->bytes_scanned = self->lexer.current_position.bytes - start_position.bytes + 1;
  result->parse_state = parse_state;
  result->first_leaf.lex_mode = lex_mode;

  LOG("lexed_lookahead sym:%s, size:%u", SYM_NAME(result->symbol), result->size.bytes);
  return result;
}

static void parser__clear_cached_token(Parser *self) {
  ts_tree_release(self->cached_token);
  self->cached_token = NULL;
}

static Tree *parser__get_lookahead(Parser *self, StackVersion version,
                                   ReusableNode *reusable_node,
                                   bool *is_fresh) {
  Length position = ts_stack_top_position(self->stack, version);

  while (reusable_node->tree) {
    if (reusable_node->byte_index > position.bytes) {
      LOG("before_reusable_node sym:%s", SYM_NAME(reusable_node->tree->symbol));
      break;
    }

    if (reusable_node->byte_index < position.bytes) {
      LOG("past_reusable sym:%s", SYM_NAME(reusable_node->tree->symbol));
      reusable_node_pop(reusable_node);
      continue;
    }

    if (reusable_node->tree->has_changes) {
      LOG("cant_reuse_changed tree:%s, size:%u",
          SYM_NAME(reusable_node->tree->symbol),
          reusable_node->tree->size.bytes);
      if (!reusable_node_breakdown(reusable_node)) {
        reusable_node_pop(reusable_node);
        parser__breakdown_top_of_stack(self, version);
      }
      continue;
    }

    if (reusable_node->tree->symbol == ts_builtin_sym_error) {
      LOG("cant_reuse_error tree:%s, size:%u",
          SYM_NAME(reusable_node->tree->symbol),
          reusable_node->tree->size.bytes);
      if (!reusable_node_breakdown(reusable_node)) {
        reusable_node_pop(reusable_node);
        parser__breakdown_top_of_stack(self, version);
      }
      continue;
    }

    if (!ts_external_token_state_eq(
          reusable_node->preceding_external_token_state,
          ts_stack_external_token_state(self->stack, version))) {
      LOG("cant_reuse_external_tokens tree:%s, size:%u",
          SYM_NAME(reusable_node->tree->symbol),
          reusable_node->tree->size.bytes);
      if (!reusable_node_breakdown(reusable_node)) {
        reusable_node_pop(reusable_node);
        parser__breakdown_top_of_stack(self, version);
      }
      continue;
    }

    Tree *result = reusable_node->tree;
    ts_tree_retain(result);
    return result;
  }

  if (self->cached_token && position.bytes == self->cached_token_byte_index) {
    ts_tree_retain(self->cached_token);
    return self->cached_token;
  }

  *is_fresh = true;
  return parser__lex(self, version);
}

static bool parser__select_tree(Parser *self, Tree *left, Tree *right) {
  if (!left)
    return true;
  if (!right)
    return false;
  if (right->error_cost < left->error_cost) {
    LOG("select_smaller_error symbol:%s, over_symbol:%s",
        SYM_NAME(right->symbol), SYM_NAME(left->symbol));
    return true;
  }
  if (left->error_cost < right->error_cost) {
    LOG("select_smaller_error symbol:%s, over_symbol:%s",
        SYM_NAME(left->symbol), SYM_NAME(right->symbol));
    return false;
  }

  int comparison = ts_tree_compare(left, right);
  switch (comparison) {
    case -1:
      LOG("select_earlier symbol:%s, over_symbol:%s", SYM_NAME(left->symbol),
          SYM_NAME(right->symbol));
      return false;
      break;
    case 1:
      LOG("select_earlier symbol:%s, over_symbol:%s", SYM_NAME(right->symbol),
          SYM_NAME(left->symbol));
      return true;
    default:
      LOG("select_existing symbol:%s, over_symbol:%s", SYM_NAME(left->symbol),
          SYM_NAME(right->symbol));
      return false;
  }
}

static bool parser__better_version_exists(Parser *self, StackVersion version,
                                          ErrorStatus my_error_status) {
  if (self->finished_tree &&
      self->finished_tree->error_cost <= my_error_status.cost)
    return true;

  for (StackVersion i = 0, n = ts_stack_version_count(self->stack); i < n; i++) {
    if (i == version || ts_stack_is_halted(self->stack, i))
      continue;

    switch (error_status_compare(my_error_status,
                                 ts_stack_error_status(self->stack, i))) {
      case -1:
        LOG("halt_other version:%u", i);
        ts_stack_halt(self->stack, i);
        break;
      case 1:
        return true;
    }
  }

  return false;
}

static void parser__shift(Parser *self, StackVersion version, TSStateId state,
                          Tree *lookahead, bool extra) {
  if (extra != lookahead->extra) {
    TSSymbolMetadata metadata =
      ts_language_symbol_metadata(self->language, lookahead->symbol);
    if (metadata.structural && ts_stack_version_count(self->stack) > 1) {
      lookahead = ts_tree_make_copy(lookahead);
    } else {
      ts_tree_retain(lookahead);
    }
    lookahead->extra = extra;
  } else {
    ts_tree_retain(lookahead);
  }

  bool is_pending = lookahead->child_count > 0;
  ts_stack_push(self->stack, version, lookahead, is_pending, state);
  if (lookahead->has_external_token_state) {
    ts_stack_set_external_token_state(
      self->stack, version, ts_tree_last_external_token_state(lookahead));
  }
  ts_tree_release(lookahead);
}

static bool parser__switch_children(Parser *self, Tree *tree,
                                    Tree **children, uint32_t count) {
  self->scratch_tree.symbol = tree->symbol;
  self->scratch_tree.child_count = 0;
  ts_tree_set_children(&self->scratch_tree, count, children);
  if (parser__select_tree(self, tree, &self->scratch_tree)) {
    tree->size = self->scratch_tree.size;
    tree->padding = self->scratch_tree.padding;
    tree->error_cost = self->scratch_tree.error_cost;
    tree->children = self->scratch_tree.children;
    tree->child_count = self->scratch_tree.child_count;
    tree->named_child_count = self->scratch_tree.named_child_count;
    tree->visible_child_count = self->scratch_tree.visible_child_count;
    return true;
  } else {
    return false;
  }
}

static StackPopResult parser__reduce(Parser *self, StackVersion version,
                                     TSSymbol symbol, unsigned count,
                                     bool fragile, bool allow_skipping) {
  uint32_t initial_version_count = ts_stack_version_count(self->stack);

  StackPopResult pop = ts_stack_pop_count(self->stack, version, count);
  if (pop.stopped_at_error)
    return pop;

  const TSLanguage *language = self->language;
  TSSymbolMetadata metadata = ts_language_symbol_metadata(language, symbol);

  for (uint32_t i = 0; i < pop.slices.size; i++) {
    StackSlice slice = pop.slices.contents[i];

    // Extra tokens on top of the stack should not be included in this new parent
    // node. They will be re-pushed onto the stack after the parent node is
    // created and pushed.
    uint32_t child_count = slice.trees.size;
    while (child_count > 0 && slice.trees.contents[child_count - 1]->extra)
      child_count--;

    Tree *parent = ts_tree_make_node(symbol, child_count, slice.trees.contents, metadata);

    // This pop operation may have caused multiple stack versions to collapse
    // into one, because they all diverged from a common state. In that case,
    // choose one of the arrays of trees to be the parent node's children, and
    // delete the rest of the tree arrays.
    while (i + 1 < pop.slices.size) {
      StackSlice next_slice = pop.slices.contents[i + 1];
      if (next_slice.version != slice.version)
        break;
      i++;

      uint32_t child_count = next_slice.trees.size;
      while (child_count > 0 && next_slice.trees.contents[child_count - 1]->extra)
        child_count--;

      if (parser__switch_children(self, parent, next_slice.trees.contents, child_count)) {
        ts_tree_array_delete(&slice.trees);
        slice = next_slice;
      } else {
        ts_tree_array_delete(&next_slice.trees);
      }
    }

    TSStateId state = ts_stack_top_state(self->stack, slice.version);
    TSStateId next_state = ts_language_next_state(language, state, symbol);
    if (fragile || self->is_split || pop.slices.size > 1 || initial_version_count > 1) {
      parent->fragile_left = true;
      parent->fragile_right = true;
      parent->parse_state = TS_TREE_STATE_NONE;
    } else {
      parent->parse_state = state;
    }

    // If this pop operation terminated at the end of an error region, then
    // create two stack versions: one in which the parent node is interpreted
    // normally, and one in which the parent node is skipped.
    if (state == ERROR_STATE && allow_skipping && child_count > 1) {
      StackVersion other_version = ts_stack_copy_version(self->stack, slice.version);

      ts_stack_push(self->stack, other_version, parent, false, ERROR_STATE);
      for (uint32_t j = parent->child_count; j < slice.trees.size; j++) {
        Tree *tree = slice.trees.contents[j];
        ts_stack_push(self->stack, other_version, tree, false, ERROR_STATE);
      }

      ErrorStatus error_status = ts_stack_error_status(self->stack, other_version);
      if (parser__better_version_exists(self, version, error_status))
        ts_stack_remove_version(self->stack, other_version);
    }

    // Push the parent node onto the stack, along with any extra tokens that
    // were previously on top of the stack.
    parser__push(self, slice.version, parent, next_state);
    for (uint32_t j = parent->child_count; j < slice.trees.size; j++) {
      Tree *tree = slice.trees.contents[j];
      parser__push(self, slice.version, tree, next_state);
    }
  }

  for (StackVersion i = initial_version_count; i < ts_stack_version_count(self->stack); i++) {
    for (StackVersion j = initial_version_count; j < i; j++) {
      if (ts_stack_merge(self->stack, j, i)) {
        i--;
        break;
      }
    }
  }

  return pop;
}

static inline const TSParseAction *parser__reductions_after_sequence(
  Parser *self, TSStateId start_state, const TreeArray *trees_below,
  uint32_t tree_count_below, const TreeArray *trees_above,
  TSSymbol lookahead_symbol, uint32_t *count) {
  TSStateId state = start_state;
  uint32_t child_count = 0;
  *count = 0;

  for (uint32_t i = 0; i < trees_below->size; i++) {
    if (child_count == tree_count_below)
      break;
    Tree *tree = trees_below->contents[trees_below->size - 1 - i];
    if (tree->extra) continue;
    TSStateId next_state = ts_language_next_state(self->language, state, tree->symbol);
    if (next_state == ERROR_STATE)
      return NULL;
    if (next_state != state) {
      child_count++;
      state = next_state;
    }
  }

  for (uint32_t i = 0; i < trees_above->size; i++) {
    Tree *tree = trees_above->contents[i];
    if (tree->extra) continue;
    TSStateId next_state = ts_language_next_state(self->language, state, tree->symbol);
    if (next_state == ERROR_STATE)
      return NULL;
    if (next_state != state) {
      child_count++;
      state = next_state;
    }
  }

  const TSParseAction *actions =
    ts_language_actions(self->language, state, lookahead_symbol, count);

  if (*count > 0 && actions[*count - 1].type != TSParseActionTypeReduce) {
    (*count)--;
  }

  while (*count > 0 && actions[0].params.child_count < child_count) {
    actions++;
    (*count)--;
  }

  while (*count > 0 && actions[*count - 1].params.child_count > child_count) {
    (*count)--;
  }

  return actions;
}

static StackIterateAction parser__repair_error_callback(
  void *payload, TSStateId state, TreeArray *trees, uint32_t tree_count,
  bool is_done, bool is_pending) {

  ErrorRepairSession *session = (ErrorRepairSession *)payload;
  Parser *self = session->parser;
  TSSymbol lookahead_symbol = session->lookahead_symbol;
  ReduceActionSet *repairs = &self->reduce_actions;
  TreeArray *trees_above_error = session->trees_above_error;
  uint32_t tree_count_above_error = session->tree_count_above_error;

  StackIterateAction result = StackIterateNone;

  uint32_t last_repair_count = -1;
  uint32_t repair_reduction_count = -1;
  const TSParseAction *repair_reductions = NULL;

  for (uint32_t i = 0; i < repairs->size; i++) {
    ReduceAction *repair = &repairs->contents[i];
    uint32_t count_needed_below_error = repair->count - tree_count_above_error;
    if (count_needed_below_error > tree_count)
      break;

    uint32_t skip_count = tree_count - count_needed_below_error;
    if (session->found_repair && skip_count >= session->best_repair_skip_count) {
      array_erase(repairs, i--);
      continue;
    }

    TSStateId state_after_repair = ts_language_next_state(self->language, state, repair->symbol);
    if (state == ERROR_STATE || state_after_repair == ERROR_STATE)
      continue;

    uint32_t action_count;
    ts_language_actions(self->language, state_after_repair, lookahead_symbol, &action_count);
    if (action_count == 0)
      continue;

    if (count_needed_below_error != last_repair_count) {
      last_repair_count = count_needed_below_error;
      repair_reductions = parser__reductions_after_sequence(
        self, state, trees, count_needed_below_error, trees_above_error,
        lookahead_symbol, &repair_reduction_count);
    }

    for (uint32_t j = 0; j < repair_reduction_count; j++) {
      if (repair_reductions[j].params.symbol == repair->symbol) {
        result |= StackIteratePop;
        session->found_repair = true;
        session->best_repair = *repair;
        session->best_repair_skip_count = skip_count;
        session->best_repair_next_state = state_after_repair;
        array_erase(repairs, i--);
        break;
      }
    }
  }

  if (repairs->size == 0)
    result |= StackIterateStop;

  return result;
}

static bool parser__repair_error(Parser *self, StackSlice slice,
                                 TSSymbol lookahead_symbol, TableEntry entry) {
  LOG("repair_error");
  ErrorRepairSession session = {
    .parser = self,
    .lookahead_symbol = lookahead_symbol,
    .found_repair = false,
    .trees_above_error = &slice.trees,
    .tree_count_above_error = ts_tree_array_essential_count(&slice.trees),
  };

  array_clear(&self->reduce_actions);
  for (uint32_t i = 0; i < entry.action_count; i++) {
    if (entry.actions[i].type == TSParseActionTypeReduce) {
      TSSymbol symbol = entry.actions[i].params.symbol;
      uint32_t child_count = entry.actions[i].params.child_count;
      if ((child_count > session.tree_count_above_error) ||
          (child_count == session.tree_count_above_error &&
           !ts_language_symbol_metadata(self->language, symbol).visible))
        array_push(&self->reduce_actions, ((ReduceAction){
          .symbol = symbol,
          .count = child_count
        }));
    }
  }

  StackPopResult pop = ts_stack_iterate(
    self->stack, slice.version, parser__repair_error_callback, &session);

  if (!session.found_repair) {
    LOG("no_repair_found");
    ts_stack_remove_version(self->stack, slice.version);
    ts_tree_array_delete(&slice.trees);
    return false;
  }

  ReduceAction repair = session.best_repair;
  TSStateId next_state = session.best_repair_next_state;
  uint32_t skip_count = session.best_repair_skip_count;
  TSSymbol symbol = repair.symbol;

  StackSlice new_slice = array_pop(&pop.slices);
  TreeArray children = new_slice.trees;
  ts_stack_renumber_version(self->stack, new_slice.version, slice.version);

  for (uint32_t i = pop.slices.size - 1; i + 1 > 0; i--) {
    StackSlice other_slice = pop.slices.contents[i];
    ts_tree_array_delete(&other_slice.trees);
    if (other_slice.version != pop.slices.contents[i + 1].version)
      ts_stack_remove_version(self->stack, other_slice.version);
  }

  TreeArray skipped_children = ts_tree_array_remove_last_n(&children, skip_count);
  TreeArray trailing_extras = ts_tree_array_remove_trailing_extras(&skipped_children);
  Tree *error = ts_tree_make_error_node(&skipped_children);
  array_push(&children, error);
  array_push_all(&children, &trailing_extras);
  trailing_extras.size = 0;
  array_delete(&trailing_extras);

  for (uint32_t i = 0; i < slice.trees.size; i++)
    array_push(&children, slice.trees.contents[i]);
  array_delete(&slice.trees);

  Tree *parent =
    ts_tree_make_node(symbol, children.size, children.contents,
                      ts_language_symbol_metadata(self->language, symbol));
  parser__push(self, slice.version, parent, next_state);
  ts_stack_decrease_push_count(self->stack, slice.version, error->child_count);

  ErrorStatus error_status = ts_stack_error_status(self->stack, slice.version);
  if (parser__better_version_exists(self, slice.version, error_status)) {
    LOG("no_better_repair_found");
    ts_stack_halt(self->stack, slice.version);
    return false;
  } else {
    LOG("repair_found sym:%s, child_count:%u, cost:%u", SYM_NAME(symbol),
        repair.count, parent->error_cost);
    return true;
  }
}

static void parser__start(Parser *self, TSInput input, Tree *previous_tree) {
  if (previous_tree) {
    LOG("parse_after_edit");
  } else {
    LOG("new_parse");
  }

  if (self->language->external_scanner.reset) {
    self->language->external_scanner.reset(self->external_scanner_payload);
  }

  ts_lexer_set_input(&self->lexer, input);
  ts_stack_clear(self->stack);
  self->reusable_node = reusable_node_new(previous_tree);
  self->cached_token = NULL;
  self->finished_tree = NULL;
}

static void parser__accept(Parser *self, StackVersion version,
                           Tree *lookahead) {
  lookahead->extra = true;
  assert(lookahead->symbol == ts_builtin_sym_end);
  ts_stack_push(self->stack, version, lookahead, false, 1);
  StackPopResult pop = ts_stack_pop_all(self->stack, version);

  for (uint32_t i = 0; i < pop.slices.size; i++) {
    StackSlice slice = pop.slices.contents[i];
    TreeArray trees = slice.trees;

    Tree *root = NULL;
    if (trees.size == 1) {
      root = trees.contents[0];
      array_delete(&trees);
    } else {
      for (uint32_t j = trees.size - 1; j + 1 > 0; j--) {
        Tree *child = trees.contents[j];
        if (!child->extra) {
          root = ts_tree_make_copy(child);
          root->child_count = 0;
          for (uint32_t k = 0; k < child->child_count; k++)
            ts_tree_retain(child->children[k]);
          array_splice(&trees, j, 1, child->child_count, child->children);
          ts_tree_set_children(root, trees.size, trees.contents);
          ts_tree_release(child);
          break;
        }
      }
    }

    if (parser__select_tree(self, self->finished_tree, root)) {
      ts_tree_release(self->finished_tree);
      assert(root->ref_count > 0);
      self->finished_tree = root;
    } else {
      ts_tree_release(root);
    }
  }

  ts_stack_remove_version(self->stack, pop.slices.contents[0].version);
  ts_stack_halt(self->stack, version);
}

static bool parser__do_potential_reductions(Parser *self, StackVersion version) {
  bool has_shift_action = false;
  TSStateId state = ts_stack_top_state(self->stack, version);
  uint32_t previous_version_count = ts_stack_version_count(self->stack);

  array_clear(&self->reduce_actions);
  for (TSSymbol symbol = 0; symbol < self->language->token_count; symbol++) {
    TableEntry entry;
    ts_language_table_entry(self->language, state, symbol, &entry);
    for (uint32_t i = 0; i < entry.action_count; i++) {
      TSParseAction action = entry.actions[i];
      if (action.extra)
        continue;
      switch (action.type) {
        case TSParseActionTypeShift:
        case TSParseActionTypeRecover:
          has_shift_action = true;
          break;
        case TSParseActionTypeReduce:
          if (action.params.child_count > 0)
            ts_reduce_action_set_add(&self->reduce_actions, (ReduceAction){
              .symbol = action.params.symbol,
              .count = action.params.child_count,
            });
        default:
          break;
      }
    }
  }

  bool did_reduce = false;
  for (uint32_t i = 0; i < self->reduce_actions.size; i++) {
    ReduceAction action = self->reduce_actions.contents[i];
    StackPopResult reduction =
      parser__reduce(self, version, action.symbol, action.count, true, false);
    if (reduction.stopped_at_error) {
      ts_tree_array_delete(&reduction.slices.contents[0].trees);
      ts_stack_remove_version(self->stack, reduction.slices.contents[0].version);
      continue;
    } else {
      did_reduce = true;
    }
  }

  if (did_reduce) {
    if (has_shift_action) {
      return true;
    } else {
      ts_stack_renumber_version(self->stack, previous_version_count, version);
      return false;
    }
  } else {
    return true;
  }
}

static StackIterateAction parser__skip_preceding_trees_callback(
  void *payload, TSStateId state, TreeArray *trees, uint32_t tree_count,
  bool is_done, bool is_pending) {
  if (tree_count > 0 && state != ERROR_STATE) {
    uint32_t bytes_skipped = 0;
    for (uint32_t i = 0; i < trees->size; i++) {
      bytes_skipped += ts_tree_total_bytes(trees->contents[i]);
    }
    if (bytes_skipped == 0) return StackIterateNone;
    SkipPrecedingTreesSession *session = payload;
    Parser *self = session->parser;
    TSSymbol lookahead_symbol = session->lookahead_symbol;
    uint32_t action_count;
    const TSParseAction *actions =
      ts_language_actions(self->language, state, lookahead_symbol, &action_count);
    if (action_count > 0 && actions[0].type == TSParseActionTypeReduce) {
      return StackIteratePop | StackIterateStop;
    }
  }
  return StackIterateNone;
}

static bool parser__skip_preceding_trees(Parser *self, StackVersion version,
                                          TSSymbol lookahead_symbol) {
  SkipPrecedingTreesSession session = { self, lookahead_symbol };
  StackPopResult pop = ts_stack_iterate(
    self->stack, version, parser__skip_preceding_trees_callback, &session);

  StackVersion previous_version = STACK_VERSION_NONE;
  for (uint32_t i = 0; i < pop.slices.size; i++) {
    StackSlice slice = pop.slices.contents[i];
    if (slice.version == previous_version) {
      ts_tree_array_delete(&slice.trees);
      continue;
    }

    previous_version = slice.version;
    Tree *error = ts_tree_make_error_node(&slice.trees);
    error->extra = true;
    TSStateId state = ts_stack_top_state(self->stack, slice.version);
    parser__push(self, slice.version, error, state);
  }

  return pop.slices.size > 0;
}

static void parser__handle_error(Parser *self, StackVersion version,
                                 TSSymbol lookahead_symbol) {
  // If there are other stack versions that are clearly better than this one,
  // just halt this version.
  ErrorStatus error_status = ts_stack_error_status(self->stack, version);
  error_status.count++;
  if (parser__better_version_exists(self, version, error_status)) {
    ts_stack_halt(self->stack, version);
    LOG("bail_on_error");
    return;
  }

  LOG("handle_error");

  // If the current lookahead symbol would have been valid in some previous
  // state on the stack, create one stack version that repairs the error
  // immediately by simply skipping all of the trees that came after that state.
  if (parser__skip_preceding_trees(self, version, lookahead_symbol)) {
    LOG("skip_preceding_trees");
    LOG_STACK();
  }

  // Perform any reductions that could have happened in this state, regardless
  // of the lookahead.
  uint32_t previous_version_count = ts_stack_version_count(self->stack);
  for (StackVersion v = version; v < ts_stack_version_count(self->stack);) {
    if (parser__do_potential_reductions(self, v)) {
      if (v == version) {
        v = previous_version_count;
      } else {
        v++;
      }
    }
  }

  // Push a discontinuity onto the stack. Merge all of the stack versions that
  // were created in the previous step.
  ts_stack_push(self->stack, version, NULL, false, ERROR_STATE);
  while (ts_stack_version_count(self->stack) > previous_version_count) {
    ts_stack_push(self->stack, previous_version_count, NULL, false, ERROR_STATE);
    assert(ts_stack_merge(self->stack, version, previous_version_count));
  }
}

static void parser__halt_parse(Parser *self) {
  LOG("halting_parse");
  LOG_STACK();

  ts_lexer_advance_to_end(&self->lexer);
  Length remaining_length = length_sub(
    self->lexer.current_position,
    ts_stack_top_position(self->stack, 0)
  );

  Tree *filler_node = ts_tree_make_error(remaining_length, length_zero(), 0);
  filler_node->visible = false;
  parser__push(self, 0, filler_node, 0);

  TreeArray children = array_new();
  Tree *root_error = ts_tree_make_error_node(&children);
  parser__push(self, 0, root_error, 0);

  TSSymbolMetadata metadata = ts_language_symbol_metadata(self->language, ts_builtin_sym_end);
  Tree *eof = ts_tree_make_leaf(ts_builtin_sym_end, length_zero(), length_zero(), metadata);
  parser__accept(self, 0, eof);
  ts_tree_release(eof);
}

static void parser__recover(Parser *self, StackVersion version, TSStateId state,
                            Tree *lookahead) {
  if (lookahead->symbol == ts_builtin_sym_end) {
    LOG("recover_eof");
    TreeArray children = array_new();
    Tree *parent = ts_tree_make_error_node(&children);
    parser__push(self, version, parent, 1);
    parser__accept(self, version, lookahead);
  }

  LOG("recover state:%u", state);

  StackVersion new_version = ts_stack_copy_version(self->stack, version);

  parser__shift(
    self, new_version, ERROR_STATE, lookahead,
    ts_language_symbol_metadata(self->language, lookahead->symbol).extra);
  ErrorStatus error_status = ts_stack_error_status(self->stack, new_version);
  if (parser__better_version_exists(self, version, error_status)) {
    ts_stack_remove_version(self->stack, new_version);
    LOG("bail_on_recovery");
  }

  parser__shift(self, version, state, lookahead, false);
}

static void parser__advance(Parser *self, StackVersion version,
                            ReusableNode *reusable_node) {
  bool validated_lookahead = false;
  Tree *lookahead = parser__get_lookahead(self, version, reusable_node, &validated_lookahead);

  for (;;) {
    TSStateId state = ts_stack_top_state(self->stack, version);

    TableEntry table_entry;
    ts_language_table_entry(self->language, state, lookahead->first_leaf.symbol, &table_entry);

    if (!validated_lookahead) {
      if (!parser__can_reuse(self, state, lookahead, &table_entry)) {
        if (lookahead == reusable_node->tree) {
          reusable_node_pop_leaf(reusable_node);
        } else {
          parser__clear_cached_token(self);
        }

        ts_tree_release(lookahead);
        lookahead = parser__get_lookahead(self, version, reusable_node, &validated_lookahead);
        continue;
      }

      validated_lookahead = true;
      LOG("reused_lookahead sym:%s, size:%u", SYM_NAME(lookahead->symbol), lookahead->size.bytes);
    }

    bool reduction_stopped_at_error = false;
    StackVersion last_reduction_version = STACK_VERSION_NONE;

    for (uint32_t i = 0; i < table_entry.action_count; i++) {
      TSParseAction action = table_entry.actions[i];

      switch (action.type) {
        case TSParseActionTypeShift: {
          bool extra = action.extra;
          TSStateId next_state;

          if (action.extra) {
            next_state = state;
            LOG("shift_extra");
          } else {
            next_state = action.params.to_state;
            LOG("shift state:%u", next_state);
          }

          if (lookahead->child_count > 0) {
            if (parser__breakdown_lookahead(self, &lookahead, state, reusable_node)) {
              if (!parser__can_reuse(self, state, lookahead, &table_entry)) {
                reusable_node_pop(reusable_node);
                ts_tree_release(lookahead);
                lookahead = parser__get_lookahead(self, version, reusable_node, &validated_lookahead);
              }
            }

            next_state = ts_language_next_state(self->language, state, lookahead->symbol);
          }

          parser__shift(self, version, next_state, lookahead, extra);

          if (lookahead == reusable_node->tree)
            reusable_node_pop(reusable_node);

          ts_tree_release(lookahead);
          return;
        }

        case TSParseActionTypeReduce: {
          if (reduction_stopped_at_error)
            continue;

          unsigned child_count = action.params.child_count;
          TSSymbol symbol = action.params.symbol;
          bool fragile = action.fragile;

          LOG("reduce sym:%s, child_count:%u", SYM_NAME(symbol), child_count);

          StackPopResult reduction =
            parser__reduce(self, version, symbol, child_count, fragile, true);
          StackSlice slice = *array_front(&reduction.slices);
          if (reduction.stopped_at_error) {
            reduction_stopped_at_error = true;
            if (!parser__repair_error(self, slice, lookahead->first_leaf.symbol,
                                      table_entry))
              break;
          }

          last_reduction_version = slice.version;
          break;
        }

        case TSParseActionTypeAccept: {
          if (ts_stack_error_status(self->stack, version).count > 0)
            continue;

          LOG("accept");
          parser__accept(self, version, lookahead);
          ts_tree_release(lookahead);
          return;
        }

        case TSParseActionTypeRecover: {
          while (lookahead->child_count > 0) {
            reusable_node_breakdown(reusable_node);
            ts_tree_release(lookahead);
            lookahead = reusable_node->tree;
            ts_tree_retain(lookahead);
          }

          parser__recover(self, version, action.params.to_state, lookahead);
          if (lookahead == reusable_node->tree)
            reusable_node_pop(reusable_node);
          ts_tree_release(lookahead);
          return;
        }
      }
    }

    if (last_reduction_version != STACK_VERSION_NONE) {
      ts_stack_renumber_version(self->stack, last_reduction_version, version);
      LOG_STACK();
      continue;
    }

    if (parser__breakdown_top_of_stack(self, version)) {
      continue;
    }

    if (state == ERROR_STATE) {
      parser__push(self, version, lookahead, ERROR_STATE);
      return;
    }

    parser__handle_error(self, version, lookahead->first_leaf.symbol);

    if (ts_stack_is_halted(self->stack, version)) {
      ts_tree_release(lookahead);
      return;
    }
  }
}

bool parser_init(Parser *self) {
  ts_lexer_init(&self->lexer);
  array_init(&self->reduce_actions);
  array_init(&self->tree_path1);
  array_init(&self->tree_path2);
  array_grow(&self->reduce_actions, 4);
  self->stack = ts_stack_new();
  self->finished_tree = NULL;
  return true;
}

void parser_set_language(Parser *self, const TSLanguage *language) {
  if (self->external_scanner_payload && self->language->external_scanner.destroy)
    self->language->external_scanner.destroy(self->external_scanner_payload);

  if (language && language->external_scanner.create)
    self->external_scanner_payload = language->external_scanner.create();
  else
    self->external_scanner_payload = NULL;

  self->language = language;
}

void parser_destroy(Parser *self) {
  if (self->stack)
    ts_stack_delete(self->stack);
  if (self->reduce_actions.contents)
    array_delete(&self->reduce_actions);
  if (self->tree_path1.contents)
    array_delete(&self->tree_path1);
  if (self->tree_path2.contents)
    array_delete(&self->tree_path2);
  parser_set_language(self, NULL);
}

Tree *parser_parse(Parser *self, TSInput input, Tree *old_tree, bool halt_on_error) {
  parser__start(self, input, old_tree);

  StackVersion version = STACK_VERSION_NONE;
  uint32_t position = 0, last_position = 0;
  ReusableNode reusable_node;

  do {
    for (version = 0; version < ts_stack_version_count(self->stack); version++) {
      reusable_node = self->reusable_node;
      last_position = position;

      while (!ts_stack_is_halted(self->stack, version)) {
        position = ts_stack_top_position(self->stack, version).chars;
        if (position > last_position || (version > 0 && position == last_position))
          break;

        LOG("process version:%d, version_count:%u, state:%d, row:%u, col:%u",
            version, ts_stack_version_count(self->stack),
            ts_stack_top_state(self->stack, version),
            ts_stack_top_position(self->stack, version).extent.row,
            ts_stack_top_position(self->stack, version).extent.column);

        parser__advance(self, version, &reusable_node);
        LOG_STACK();
      }
    }

    self->reusable_node = reusable_node;

    CondenseResult condense_result = parser__condense_stack(self);
    if (halt_on_error && (condense_result & CondenseResultAllVersionsHadError)) {
      parser__halt_parse(self);
      break;
    }

    if (condense_result & CondenseResultMadeChange) {
      LOG("condense");
      LOG_STACK();
    }

    self->is_split = (version > 1);
  } while (version != 0);

  LOG("done");
  LOG_TREE();
  ts_stack_clear(self->stack);
  parser__clear_cached_token(self);
  ts_tree_assign_parents(self->finished_tree, &self->tree_path1);
  return self->finished_tree;
}
