/**
 * @file C++ grammar for tree-sitter
 * @author Max Brunsfeld <maxbrunsfeld@gmail.com>
 * @author Amaan Qureshi <amaanq12@gmail.com>
 * @author John Drouhard <john@drouhard.dev>
 * @license MIT
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

const C = require('tree-sitter-c/grammar');

const PREC = Object.assign(C.PREC, {
  LAMBDA: 18,
  NEW: C.PREC.CALL + 1,
  STRUCTURED_BINDING: -1,
  THREE_WAY: C.PREC.RELATIONAL + 1,
});

const FOLD_OPERATORS = [
  '+', '-', '*', '/', '%',
  '^', '&', '|',
  '=', '<', '>',
  '<<', '>>',
  '+=', '-=', '*=', '/=', '%=', '^=', '&=', '|=',
  '>>=', '<<=',
  '==', '!=', '<=', '>=',
  '&&', '||',
  ',',
  '.*', '->*',
  'or', 'and', 'bitor', 'xor', 'bitand', 'not_eq',
];

const ASSIGNMENT_OPERATORS = [
  '=',
  '*=',
  '/=',
  '%=',
  '+=',
  '-=',
  '<<=',
  '>>=',
  '&=',
  '^=',
  '|=',
  'and_eq',
  'or_eq',
  'xor_eq',
];

module.exports = grammar(C, {
  name: 'cpp',

  externals: $ => [
    $.raw_string_delimiter,
    $.raw_string_content,
  ],

  conflicts: $ => [
    // C
    [$.type_specifier, $._declarator],
    [$.type_specifier, $.expression],
    [$.sized_type_specifier],
    [$.attributed_statement],
    [$._declaration_modifiers, $.attributed_statement],
    [$._top_level_item, $._top_level_statement],
    [$._block_item, $.statement],
    [$.type_qualifier, $.extension_expression],

    // C++
    [$.template_function, $.template_type],
    [$.template_function, $.template_type, $.expression],
    [$.template_function, $.template_type, $.qualified_identifier],
    [$.template_type, $.qualified_type_identifier],
    [$.qualified_type_identifier, $.qualified_identifier],
    [$.comma_expression, $.initializer_list],
    [$.expression, $._declarator],
    [$.expression, $.structured_binding_declarator],
    [$.expression, $._declarator, $.type_specifier],
    [$.expression, $.identifier_parameter_pack_expansion],
    [$.expression, $._lambda_capture_identifier],
    [$.expression, $._lambda_capture],
    [$.expression, $.structured_binding_declarator, $._lambda_capture_identifier],
    [$.structured_binding_declarator, $._lambda_capture_identifier],
    [$.parameter_list, $.argument_list],
    [$.type_specifier, $.call_expression],
    [$._declaration_specifiers, $._constructor_specifiers],
    [$._binary_fold_operator, $._fold_operator],
    [$._function_declarator_seq],
    [$.type_specifier, $.sized_type_specifier],
    [$.initializer_pair, $.comma_expression],
    [$.expression_statement, $._for_statement_body],
    [$.init_statement, $._for_statement_body],
    [$.field_expression, $.template_method, $.template_type],
    [$.qualified_field_identifier, $.template_method, $.template_type],
  ],

  inline: ($, original) => original.concat([
    $._namespace_identifier,
  ]),

  precedences: $ => [
    [$.argument_list, $.type_qualifier],
    [$._expression_not_binary, $._class_name],
  ],

  rules: {
    _top_level_item: ($, original) => choice(
      ...original.members.filter((member) => member.content?.name != '_old_style_function_definition'),
      $.namespace_definition,
      $.concept_definition,
      $.namespace_alias_definition,
      $.using_declaration,
      $.alias_declaration,
      $.static_assert_declaration,
      $.template_declaration,
      $.template_instantiation,
      alias($.constructor_or_destructor_definition, $.function_definition),
      alias($.operator_cast_definition, $.function_definition),
      alias($.operator_cast_declaration, $.declaration),
    ),

    _block_item: ($, original) => choice(
      ...original.members.filter((member) => member.content?.name != '_old_style_function_definition'),
      $.namespace_definition,
      $.concept_definition,
      $.namespace_alias_definition,
      $.using_declaration,
      $.alias_declaration,
      $.static_assert_declaration,
      $.template_declaration,
      $.template_instantiation,
      alias($.constructor_or_destructor_definition, $.function_definition),
      alias($.operator_cast_definition, $.function_definition),
      alias($.operator_cast_declaration, $.declaration),
    ),

    // Types

    placeholder_type_specifier: $ => prec(1, seq(
      field('constraint', optional($.type_specifier)),
      choice($.auto, alias($.decltype_auto, $.decltype)),
    )),

    auto: _ => 'auto',
    decltype_auto: $ => seq(
      'decltype',
      '(',
      $.auto,
      ')',
    ),
    decltype: $ => seq(
      'decltype',
      '(',
      $.expression,
      ')',
    ),

    type_specifier: $ => choice(
      $.struct_specifier,
      $.union_specifier,
      $.enum_specifier,
      $.class_specifier,
      $.sized_type_specifier,
      $.primitive_type,
      $.template_type,
      $.dependent_type,
      $.placeholder_type_specifier,
      $.decltype,
      prec.right(choice(
        alias($.qualified_type_identifier, $.qualified_identifier),
        $._type_identifier,
      )),
    ),

    type_qualifier: (_, original) => choice(
      original,
      'mutable',
      'constinit',
      'consteval',
    ),

    type_descriptor: (_, original) => prec.right(original),

    // When used in a trailing return type, these specifiers can now occur immediately before
    // a compound statement. This introduces a shift/reduce conflict that needs to be resolved
    // with an associativity.
    _class_declaration: $ => seq(
      repeat(choice($.attribute_specifier, $.alignas_qualifier)),
      optional($.ms_declspec_modifier),
      repeat($.attribute_declaration),
      $._class_declaration_item,
    ),
    _class_declaration_item: $ => prec.right(seq(
      choice(
        field('name', $._class_name),
        seq(
          optional(field('name', $._class_name)),
          optional($.virtual_specifier),
          optional($.base_class_clause),
          field('body', $.field_declaration_list),
        ),
      ),
      optional($.attribute_specifier),
    )),

    class_specifier: $ => seq(
      'class',
      $._class_declaration,
    ),

    union_specifier: $ => seq(
      'union',
      $._class_declaration,
    ),

    struct_specifier: $ => seq(
      'struct',
      $._class_declaration,
    ),

    _class_name: $ => prec.right(choice(
      $._type_identifier,
      $.template_type,
      alias($.qualified_type_identifier, $.qualified_identifier),
    )),

    function_definition: ($, original) => ({
      ...original,
      members: original.members.map(
        (e) => e.name !== 'body' ?
          e :
          field('body', choice(e.content, $.try_statement))),
    }),

    declaration: $ => seq(
      $._declaration_specifiers,
      commaSep1(field('declarator', choice(
        seq(
          // C uses _declaration_declarator here for some nice macro parsing in function declarators,
          // but this causes a world of pain for C++ so we'll just stick to the normal _declarator here.
          $._declarator,
          optional($.gnu_asm_expression),
        ),
        $.init_declarator,
      ))),
      ';',
    ),

    virtual_specifier: _ => choice(
      'final', // the only legal value here for classes
      'override', // legal for functions in addition to final, plus permutations.
    ),

    _declaration_modifiers: ($, original) => choice(
      original,
      'virtual',
    ),

    explicit_function_specifier: $ => choice(
      'explicit',
      prec(PREC.CALL, seq(
        'explicit',
        '(',
        $.expression,
        ')',
      )),
    ),

    base_class_clause: $ => seq(
      ':',
      commaSep1(seq(
        repeat($.attribute_declaration),
        optional(choice(
          $.access_specifier,
          seq($.access_specifier, optional('virtual')),
          seq('virtual', optional($.access_specifier)),
        )),
        $._class_name,
        optional('...'),
      )),
    ),

    enum_specifier: $ => prec.right(seq(
      'enum',
      optional(choice('class', 'struct')),
      choice(
        seq(
          field('name', $._class_name),
          optional($._enum_base_clause),
          optional(field('body', $.enumerator_list)),
        ),
        field('body', $.enumerator_list),
      ),
      optional($.attribute_specifier),
    )),

    _enum_base_clause: $ => prec.left(seq(
      ':',
      field('base', choice(
        alias($.qualified_type_identifier, $.qualified_identifier),
        $._type_identifier,
        $.primitive_type,
        $.sized_type_specifier,
      )),
    )),

    // The `auto` storage class is removed in C++0x in order to allow for the `auto` type.
    storage_class_specifier: (_, original) => choice(
      ...original.members.filter((member) => member.value !== 'auto'),
      'thread_local',
    ),

    dependent_type: $ => prec.dynamic(-1, prec.right(seq(
      'typename',
      $.type_specifier,
    ))),

    // Declarations

    template_declaration: $ => seq(
      'template',
      field('parameters', $.template_parameter_list),
      optional($.requires_clause),
      choice(
        $._empty_declaration,
        $.alias_declaration,
        $.declaration,
        $.template_declaration,
        $.function_definition,
        $.concept_definition,
        $.friend_declaration,
        alias($.constructor_or_destructor_declaration, $.declaration),
        alias($.constructor_or_destructor_definition, $.function_definition),
        alias($.operator_cast_declaration, $.declaration),
        alias($.operator_cast_definition, $.function_definition),
      ),
    ),

    template_instantiation: $ => seq(
      'template',
      optional($._declaration_specifiers),
      field('declarator', $._declarator),
      ';',
    ),

    template_parameter_list: $ => seq(
      '<',
      commaSep(choice(
        $.parameter_declaration,
        $.optional_parameter_declaration,
        $.type_parameter_declaration,
        $.variadic_parameter_declaration,
        $.variadic_type_parameter_declaration,
        $.optional_type_parameter_declaration,
        $.template_template_parameter_declaration,
      )),
      alias(token(prec(1, '>')), '>'),
    ),

    type_parameter_declaration: $ => prec(1, seq(
      choice('typename', 'class'),
      optional($._type_identifier),
    )),

    variadic_type_parameter_declaration: $ => prec(1, seq(
      choice('typename', 'class'),
      '...',
      optional($._type_identifier),
    )),

    optional_type_parameter_declaration: $ => seq(
      choice('typename', 'class'),
      optional(field('name', $._type_identifier)),
      '=',
      field('default_type', $.type_specifier),
    ),

    template_template_parameter_declaration: $ => seq(
      'template',
      field('parameters', $.template_parameter_list),
      choice(
        $.type_parameter_declaration,
        $.variadic_type_parameter_declaration,
        $.optional_type_parameter_declaration,
      ),
    ),

    parameter_list: $ => seq(
      '(',
      commaSep(choice(
        $.parameter_declaration,
        $.optional_parameter_declaration,
        $.variadic_parameter_declaration,
        '...',
      )),
      ')',
    ),

    optional_parameter_declaration: $ => seq(
      $._declaration_specifiers,
      field('declarator', optional(choice($._declarator, $.abstract_reference_declarator))),
      '=',
      field('default_value', $.expression),
    ),

    variadic_parameter_declaration: $ => seq(
      $._declaration_specifiers,
      field('declarator', choice(
        $.variadic_declarator,
        alias($.variadic_reference_declarator, $.reference_declarator),
      )),
    ),

    variadic_declarator: $ => seq(
      '...',
      optional($.identifier),
    ),

    variadic_reference_declarator: $ => seq(
      choice('&&', '&'),
      $.variadic_declarator,
    ),

    init_declarator: ($, original) => choice(
      original,
      seq(
        field('declarator', $._declarator),
        field('value', choice(
          $.argument_list,
          $.initializer_list,
        )),
      ),
    ),

    operator_cast: $ => prec.right(1, seq(
      'operator',
      $._declaration_specifiers,
      field('declarator', $._abstract_declarator),
    )),

    // Avoid ambiguity between compound statement and initializer list in a construct like:
    //   A b {};
    compound_statement: (_, original) => prec(-1, original),

    field_initializer_list: $ => seq(
      ':',
      commaSep1($.field_initializer),
    ),

    field_initializer: $ => prec(1, seq(
      choice(
        $._field_identifier,
        $.template_method,
        alias($.qualified_field_identifier, $.qualified_identifier),
      ),
      choice($.initializer_list, $.argument_list),
      optional('...'),
    )),

    _field_declaration_list_item: ($, original) => choice(
      original,
      $.template_declaration,
      alias($.inline_method_definition, $.function_definition),
      alias($.constructor_or_destructor_definition, $.function_definition),
      alias($.constructor_or_destructor_declaration, $.declaration),
      alias($.operator_cast_definition, $.function_definition),
      alias($.operator_cast_declaration, $.declaration),
      $.friend_declaration,
      seq($.access_specifier, ':'),
      $.alias_declaration,
      $.using_declaration,
      $.type_definition,
      $.static_assert_declaration,
      ';',
    ),

    field_declaration: $ => seq(
      $._declaration_specifiers,
      commaSep(seq(
        field('declarator', $._field_declarator),
        optional(choice(
          $.bitfield_clause,
          field('default_value', $.initializer_list),
          seq('=', field('default_value', choice($.expression, $.initializer_list))),
        )),
      )),
      optional($.attribute_specifier),
      ';',
    ),

    inline_method_definition: $ => seq(
      $._declaration_specifiers,
      field('declarator', $._field_declarator),
      choice(
        field('body', choice($.compound_statement, $.try_statement)),
        $.default_method_clause,
        $.delete_method_clause,
        $.pure_virtual_clause,
      ),
    ),

    _constructor_specifiers: $ => choice(
      $._declaration_modifiers,
      $.explicit_function_specifier,
    ),

    operator_cast_definition: $ => seq(
      repeat($._constructor_specifiers),
      field('declarator', choice(
        $.operator_cast,
        alias($.qualified_operator_cast_identifier, $.qualified_identifier),
      )),
      field('body', choice($.compound_statement, $.try_statement)),
    ),

    operator_cast_declaration: $ => prec(1, seq(
      repeat($._constructor_specifiers),
      field('declarator', choice(
        $.operator_cast,
        alias($.qualified_operator_cast_identifier, $.qualified_identifier),
      )),
      optional(seq('=', field('default_value', $.expression))),
      ';',
    )),

    constructor_try_statement: $ => seq(
      'try',
      optional($.field_initializer_list),
      field('body', $.compound_statement),
      repeat1($.catch_clause),
    ),

    constructor_or_destructor_definition: $ => seq(
      repeat($._constructor_specifiers),
      field('declarator', $.function_declarator),
      choice(
        seq(
          optional($.field_initializer_list),
          field('body', $.compound_statement),
        ),
        alias($.constructor_try_statement, $.try_statement),
        $.default_method_clause,
        $.delete_method_clause,
        $.pure_virtual_clause,
      ),
    ),

    constructor_or_destructor_declaration: $ => seq(
      repeat($._constructor_specifiers),
      field('declarator', $.function_declarator),
      ';',
    ),

    default_method_clause: _ => seq('=', 'default', ';'),
    delete_method_clause: _ => seq('=', 'delete', ';'),
    pure_virtual_clause: _ => seq('=', /0/, ';'),

    friend_declaration: $ => seq(
      optional('constexpr'),
      'friend',
      choice(
        $.declaration,
        $.function_definition,
        seq(
          optional(choice(
            'class',
            'struct',
            'union',
          )),
          $._class_name, ';',
        ),
      ),
    ),

    access_specifier: _ => choice(
      'public',
      'private',
      'protected',
    ),

    _declarator: ($, original) => choice(
      original,
      $.reference_declarator,
      $.qualified_identifier,
      $.template_function,
      $.operator_name,
      $.destructor_name,
      $.structured_binding_declarator,
    ),

    _field_declarator: ($, original) => choice(
      original,
      alias($.reference_field_declarator, $.reference_declarator),
      $.template_method,
      $.operator_name,
    ),

    _type_declarator: ($, original) => choice(
      original,
      alias($.reference_type_declarator, $.reference_declarator),
    ),

    _abstract_declarator: ($, original) => choice(
      original,
      $.abstract_reference_declarator,
    ),

    reference_declarator: $ => prec.dynamic(1, prec.right(seq(choice('&', '&&'), $._declarator))),
    reference_field_declarator: $ => prec.dynamic(1, prec.right(seq(choice('&', '&&'), $._field_declarator))),
    reference_type_declarator: $ => prec.dynamic(1, prec.right(seq(choice('&', '&&'), $._type_declarator))),
    abstract_reference_declarator: $ => prec.right(seq(choice('&', '&&'), optional($._abstract_declarator))),

    structured_binding_declarator: $ => prec.dynamic(PREC.STRUCTURED_BINDING, seq(
      '[', commaSep1($.identifier), ']',
    )),

    ref_qualifier: _ => choice('&', '&&'),

    _function_declarator_seq: $ => seq(
      field('parameters', $.parameter_list),
      optional($._function_attributes_start),
      optional($.ref_qualifier),
      optional($._function_exception_specification),
      optional($._function_attributes_end),
      optional($.trailing_return_type),
      optional($._function_postfix),
    ),

    _function_attributes_start: $ => prec(1, choice(
      seq(repeat1($.attribute_specifier), repeat($.type_qualifier)),
      seq(repeat($.attribute_specifier), repeat1($.type_qualifier)),
    )),

    _function_exception_specification: $ => choice(
      $.noexcept,
      $.throw_specifier,
    ),

    _function_attributes_end: $ => prec.right(seq(
      optional($.gnu_asm_expression),
      choice(
        seq(repeat1($.attribute_specifier), repeat($.attribute_declaration)),
        seq(repeat($.attribute_specifier), repeat1($.attribute_declaration)),
      ),
    )),

    _function_postfix: $ => prec.right(choice(
      repeat1($.virtual_specifier),
      $.requires_clause,
    )),

    function_declarator: $ => prec.dynamic(1, seq(
      field('declarator', $._declarator),
      $._function_declarator_seq,
    )),

    function_field_declarator: $ => prec.dynamic(1, seq(
      field('declarator', $._field_declarator),
      $._function_declarator_seq,
    )),

    abstract_function_declarator: $ => seq(
      field('declarator', optional($._abstract_declarator)),
      $._function_declarator_seq,
    ),

    trailing_return_type: $ => seq('->', $.type_descriptor),

    noexcept: $ => prec.right(seq(
      'noexcept',
      optional(
        seq(
          '(',
          optional($.expression),
          ')',
        ),
      ),
    )),

    throw_specifier: $ => seq(
      'throw',
      seq(
        '(',
        commaSep($.type_descriptor),
        ')',
      ),
    ),

    template_type: $ => seq(
      field('name', $._type_identifier),
      field('arguments', $.template_argument_list),
    ),

    template_method: $ => seq(
      field('name', choice($._field_identifier, $.operator_name)),
      field('arguments', $.template_argument_list),
    ),

    template_function: $ => seq(
      field('name', $.identifier),
      field('arguments', $.template_argument_list),
    ),

    template_argument_list: $ => seq(
      '<',
      commaSep(choice(
        prec.dynamic(3, $.type_descriptor),
        prec.dynamic(2, alias($.type_parameter_pack_expansion, $.parameter_pack_expansion)),
        prec.dynamic(1, $.expression),
      )),
      alias(token(prec(1, '>')), '>'),
    ),

    namespace_definition: $ => seq(
      optional('inline'),
      'namespace',
      optional($.attribute_declaration),
      field('name', optional(
        choice(
          $._namespace_identifier,
          $.nested_namespace_specifier,
        ))),
      field('body', $.declaration_list),
    ),

    namespace_alias_definition: $ => seq(
      'namespace',
      field('name', $._namespace_identifier),
      '=',
      choice(
        $._namespace_identifier,
        $.nested_namespace_specifier,
      ),
      ';',
    ),

    _namespace_specifier: $ => seq(
      optional('inline'),
      $._namespace_identifier,
    ),

    nested_namespace_specifier: $ => prec(1, seq(
      optional($._namespace_specifier),
      '::',
      choice(
        $.nested_namespace_specifier,
        $._namespace_specifier,
      ),
    )),

    using_declaration: $ => seq(
      'using',
      optional(choice('namespace', 'enum')),
      choice(
        $.identifier,
        $.qualified_identifier,
      ),
      ';',
    ),

    alias_declaration: $ => seq(
      'using',
      field('name', $._type_identifier),
      repeat($.attribute_declaration),
      '=',
      field('type', $.type_descriptor),
      ';',
    ),

    static_assert_declaration: $ => seq(
      'static_assert',
      '(',
      field('condition', $.expression),
      optional(seq(
        ',',
        field('message', $._string),
      )),
      ')',
      ';',
    ),

    concept_definition: $ => seq(
      'concept',
      field('name', $.identifier),
      '=',
      $.expression,
      ';',
    ),

    // Statements

    _top_level_statement: ($, original) => choice(
      original,
      $.co_return_statement,
      $.co_yield_statement,
      $.for_range_loop,
      $.try_statement,
      $.throw_statement,
    ),

    _non_case_statement: ($, original) => choice(
      original,
      $.co_return_statement,
      $.co_yield_statement,
      $.for_range_loop,
      $.try_statement,
      $.throw_statement,
    ),

    switch_statement: $ => seq(
      'switch',
      field('condition', $.condition_clause),
      field('body', $.compound_statement),
    ),

    while_statement: $ => seq(
      'while',
      field('condition', $.condition_clause),
      field('body', $.statement),
    ),

    if_statement: $ => prec.right(seq(
      'if',
      optional('constexpr'),
      field('condition', $.condition_clause),
      field('consequence', $.statement),
      optional(field('alternative', $.else_clause)),
    )),

    // Using prec(1) instead of prec.dynamic(1) causes issues with the
    // range loop's declaration specifiers if `int` is passed in, it'll
    // always prefer the standard for loop and give us a parse error.
    _for_statement_body: ($, original) => prec.dynamic(1, original),
    for_range_loop: $ => seq(
      'for',
      '(',
      $._for_range_loop_body,
      ')',
      field('body', $.statement),
    ),
    _for_range_loop_body: $ => seq(
      field('initializer', optional($.init_statement)),
      $._declaration_specifiers,
      field('declarator', $._declarator),
      ':',
      field('right', choice(
        $.expression,
        $.initializer_list,
      )),
    ),

    init_statement: $ => choice(
      $.alias_declaration,
      $.type_definition,
      $.declaration,
      $.expression_statement,
    ),

    condition_clause: $ => seq(
      '(',
      field('initializer', optional($.init_statement)),
      field('value', choice(
        $.expression,
        $.comma_expression,
        alias($.condition_declaration, $.declaration),
      )),
      ')',
    ),

    condition_declaration: $ => seq(
      $._declaration_specifiers,
      field('declarator', $._declarator),
      choice(
        seq(
          '=',
          field('value', $.expression),
        ),
        field('value', $.initializer_list),
      ),
    ),

    return_statement: ($, original) => seq(
      choice(
        original,
        seq('return', $.initializer_list, ';'),
      ),
    ),

    co_return_statement: $ => seq(
      'co_return',
      optional($.expression),
      ';',
    ),

    co_yield_statement: $ => seq(
      'co_yield',
      $.expression,
      ';',
    ),

    throw_statement: $ => seq(
      'throw',
      optional($.expression),
      ';',
    ),

    try_statement: $ => seq(
      'try',
      field('body', $.compound_statement),
      repeat1($.catch_clause),
    ),

    catch_clause: $ => seq(
      'catch',
      field('parameters', $.parameter_list),
      field('body', $.compound_statement),
    ),

    // Expressions

    _expression_not_binary: ($, original) => choice(
      original,
      $.co_await_expression,
      $.requires_expression,
      $.requires_clause,
      $.template_function,
      $.qualified_identifier,
      $.new_expression,
      $.delete_expression,
      $.lambda_expression,
      $.parameter_pack_expansion,
      $.this,
      $.user_defined_literal,
      $.fold_expression,
    ),

    _string: $ => choice(
      $.string_literal,
      $.raw_string_literal,
      $.concatenated_string,
    ),

    raw_string_literal: $ => seq(
      choice('R"', 'LR"', 'uR"', 'UR"', 'u8R"'),
      choice(
        seq(
          field('delimiter', $.raw_string_delimiter),
          '(',
          $.raw_string_content,
          ')',
          $.raw_string_delimiter,
        ),
        seq('(', $.raw_string_content, ')'),
      ),
      '"',
    ),

    subscript_expression: $ => prec(PREC.SUBSCRIPT, seq(
      field('argument', $.expression),
      field('indices', $.subscript_argument_list),
    )),

    subscript_argument_list: $ => seq(
      '[',
      commaSep(choice($.expression, $.initializer_list)),
      ']',
    ),

    call_expression: ($, original) => choice(original, seq(
      field('function', $.primitive_type),
      field('arguments', $.argument_list),
    )),

    co_await_expression: $ => prec.left(PREC.UNARY, seq(
      field('operator', 'co_await'),
      field('argument', $.expression),
    )),

    new_expression: $ => prec.right(PREC.NEW, seq(
      optional('::'),
      'new',
      field('placement', optional($.argument_list)),
      field('type', $.type_specifier),
      field('declarator', optional($.new_declarator)),
      field('arguments', optional(choice(
        $.argument_list,
        $.initializer_list,
      ))),
    )),

    new_declarator: $ => prec.right(seq(
      '[',
      field('length', $.expression),
      ']',
      optional($.new_declarator),
    )),

    delete_expression: $ => seq(
      optional('::'),
      'delete',
      optional(seq('[', ']')),
      $.expression,
    ),

    field_expression: $ => seq(
      prec(PREC.FIELD, seq(
        field('argument', $.expression),
        field('operator', choice('.', '.*', '->')),
      )),
      field('field', choice(
        prec.dynamic(1, $._field_identifier),
        alias($.qualified_field_identifier, $.qualified_identifier),
        $.destructor_name,
        $.template_method,
        alias($.dependent_field_identifier, $.dependent_name),
      )),
    ),

    type_requirement: $ => seq('typename', $._class_name),

    compound_requirement: $ => seq(
      '{', $.expression, '}',
      optional('noexcept'),
      optional($.trailing_return_type),
      ';',
    ),

    _requirement: $ => choice(
      alias($.expression_statement, $.simple_requirement),
      $.type_requirement,
      $.compound_requirement,
    ),

    requirement_seq: $ => seq('{', repeat($._requirement), '}'),

    constraint_conjunction: $ => prec.left(PREC.LOGICAL_AND, seq(
      field('left', $._requirement_clause_constraint),
      field('operator', choice('&&', 'and')),
      field('right', $._requirement_clause_constraint)),
    ),

    constraint_disjunction: $ => prec.left(PREC.LOGICAL_OR, seq(
      field('left', $._requirement_clause_constraint),
      field('operator', choice('||', 'or')),
      field('right', $._requirement_clause_constraint)),
    ),

    _requirement_clause_constraint: $ => choice(
      // Primary expressions"
      $.true,
      $.false,
      $._class_name,
      $.fold_expression,
      $.lambda_expression,
      $.requires_expression,

      // Parenthesized expressions
      seq('(', $.expression, ')'),

      // conjunction or disjunction of the above
      $.constraint_conjunction,
      $.constraint_disjunction,
    ),

    requires_clause: $ => seq(
      'requires',
      field('constraint', $._requirement_clause_constraint),
    ),

    requires_parameter_list: $ => seq(
      '(',
      commaSep(choice(
        $.parameter_declaration,
        $.optional_parameter_declaration,
        $.variadic_parameter_declaration,
      )),
      ')',
    ),

    requires_expression: $ => seq(
      'requires',
      field('parameters', optional(alias($.requires_parameter_list, $.parameter_list))),
      field('requirements', $.requirement_seq),
    ),

    lambda_expression: $ => seq(
      field('captures', $.lambda_capture_specifier),
      optional(seq(
        field('template_parameters', $.template_parameter_list),
        optional(field('constraint', $.requires_clause)),
      )),
      optional(field('declarator', $.abstract_function_declarator)),
      field('body', $.compound_statement),
    ),

    lambda_capture_specifier: $ => prec(PREC.LAMBDA, seq(
      '[',
      choice(
        $.lambda_default_capture,
        commaSep($._lambda_capture),
        seq(
          $.lambda_default_capture,
          ',', commaSep1($._lambda_capture),
        ),
      ),
      ']',
    )),

    lambda_default_capture: _ => choice('=', '&'),

    _lambda_capture_identifier: $ => seq(
      optional('&'),
      choice(
        $.identifier,
        $.qualified_identifier,
        alias($.identifier_parameter_pack_expansion, $.parameter_pack_expansion),
      ),
    ),

    lambda_capture_initializer: $ => seq(
      optional('&'),
      optional('...'),
      field('left', $.identifier),
      '=',
      field('right', $.expression),
    ),

    _lambda_capture: $ => choice(
      seq(optional('*'), $.this),
      $._lambda_capture_identifier,
      $.lambda_capture_initializer,
    ),

    _fold_operator: _ => choice(...FOLD_OPERATORS),
    _binary_fold_operator: _ => choice(...FOLD_OPERATORS.map((operator) => seq(field('operator', operator), '...', operator))),

    _unary_left_fold: $ => seq(
      field('left', '...'),
      field('operator', $._fold_operator),
      field('right', $.expression),
    ),
    _unary_right_fold: $ => seq(
      field('left', $.expression),
      field('operator', $._fold_operator),
      field('right', '...'),
    ),
    _binary_fold: $ => seq(
      field('left', $.expression),
      $._binary_fold_operator,
      field('right', $.expression),
    ),

    fold_expression: $ => seq(
      '(',
      choice(
        $._unary_right_fold,
        $._unary_left_fold,
        $._binary_fold,
      ),
      ')',
    ),

    parameter_pack_expansion: $ => prec(-1, seq(
      field('pattern', $.expression),
      '...',
    )),

    type_parameter_pack_expansion: $ => seq(
      field('pattern', $.type_descriptor),
      '...',
    ),

    identifier_parameter_pack_expansion: $ => seq(
      field('pattern', $.identifier),
      '...',
    ),

    sizeof_expression: ($, original) => prec.right(PREC.SIZEOF, choice(
      original,
      seq(
        'sizeof', '...',
        '(',
        field('value', $.identifier),
        ')',
      ),
    )),

    unary_expression: ($, original) => choice(
      original,
      prec.left(PREC.UNARY, seq(
        field('operator', choice('not', 'compl')),
        field('argument', $.expression),
      )),
    ),

    binary_expression: ($, original) => {
      const table = [
        ['<=>', PREC.THREE_WAY],
        ['or', PREC.LOGICAL_OR],
        ['and', PREC.LOGICAL_AND],
        ['bitor', PREC.INCLUSIVE_OR],
        ['xor', PREC.EXCLUSIVE_OR],
        ['bitand', PREC.BITWISE_AND],
        ['not_eq', PREC.EQUAL],
      ];

      return choice(
        original,
        ...table.map(([operator, precedence]) => {
          return prec.left(precedence, seq(
            field('left', $.expression),
            // @ts-ignore
            field('operator', operator),
            field('right', $.expression),
          ));
        }));
    },

    // The compound_statement is added to parse macros taking statements as arguments, e.g. MYFORLOOP(1, 10, i, { foo(i); bar(i); })
    argument_list: $ => seq(
      '(',
      commaSep(choice($.expression, $.initializer_list, $.compound_statement)),
      ')',
    ),

    destructor_name: $ => prec(1, seq('~', $.identifier)),

    compound_literal_expression: ($, original) => choice(
      original,
      seq(
        field('type', choice($._class_name, $.primitive_type)),
        field('value', $.initializer_list),
      ),
    ),

    dependent_identifier: $ => seq('template', $.template_function),
    dependent_field_identifier: $ => seq('template', $.template_method),
    dependent_type_identifier: $ => seq('template', $.template_type),

    _scope_resolution: $ => prec(1, seq(
      field('scope', optional(choice(
        $._namespace_identifier,
        $.template_type,
        $.decltype,
        alias($.dependent_type_identifier, $.dependent_name),
      ))),
      '::',
    )),

    qualified_field_identifier: $ => seq(
      $._scope_resolution,
      field('name', choice(
        alias($.dependent_field_identifier, $.dependent_name),
        alias($.qualified_field_identifier, $.qualified_identifier),
        $.template_method,
        prec.dynamic(1, $._field_identifier),
      )),
    ),

    qualified_identifier: $ => seq(
      $._scope_resolution,
      field('name', choice(
        alias($.dependent_identifier, $.dependent_name),
        $.qualified_identifier,
        $.template_function,
        seq(optional('template'), $.identifier),
        $.operator_name,
        $.destructor_name,
        $.pointer_type_declarator,
      )),
    ),

    qualified_type_identifier: $ => seq(
      $._scope_resolution,
      field('name', choice(
        alias($.dependent_type_identifier, $.dependent_name),
        alias($.qualified_type_identifier, $.qualified_identifier),
        $.template_type,
        $._type_identifier,
      )),
    ),

    qualified_operator_cast_identifier: $ => seq(
      $._scope_resolution,
      field('name', choice(
        alias($.qualified_operator_cast_identifier, $.qualified_identifier),
        $.operator_cast,
      )),
    ),

    _assignment_left_expression: ($, original) => choice(
      original,
      $.qualified_identifier,
      $.user_defined_literal,
    ),

    assignment_expression: $ => prec.right(PREC.ASSIGNMENT, seq(
      field('left', $._assignment_left_expression),
      field('operator', choice(...ASSIGNMENT_OPERATORS)),
      field('right', choice($.expression, $.initializer_list)),
    )),

    _assignment_expression_lhs: $ => seq(
      field('left', $.expression),
      field('operator', choice(...ASSIGNMENT_OPERATORS)),
      field('right', choice($.expression, $.initializer_list)),
    ),

    // This prevents an ambiguity between fold expressions
    // and assignment expressions within parentheses.
    parenthesized_expression: ($, original) => choice(
      original,
      seq('(', alias($._assignment_expression_lhs, $.assignment_expression), ')'),
    ),

    operator_name: $ => prec(1, seq(
      'operator',
      choice(
        'co_await',
        '+', '-', '*', '/', '%',
        '^', '&', '|', '~',
        '!', '=', '<', '>',
        '+=', '-=', '*=', '/=', '%=', '^=', '&=', '|=',
        '<<', '>>', '>>=', '<<=',
        '==', '!=', '<=', '>=',
        '<=>',
        '&&', '||',
        '++', '--',
        ',',
        '->*',
        '->',
        '()', '[]',
        'xor', 'bitand', 'bitor', 'compl',
        'not', 'xor_eq', 'and_eq', 'or_eq', 'not_eq',
        'and', 'or',
        seq(choice('new', 'delete'), optional('[]')),
        seq('""', $.identifier),
      ),
    )),

    this: _ => 'this',

    concatenated_string: $ => prec.right(seq(
      choice($.identifier, $.string_literal, $.raw_string_literal),
      choice($.string_literal, $.raw_string_literal),
      repeat(choice($.identifier, $.string_literal, $.raw_string_literal)),
    )),

    number_literal: $ => {
      const sign = /[-\+]/;
      const separator = '\'';
      const binary = /[01]/;
      const binaryDigits = seq(repeat1(binary), repeat(seq(separator, repeat1(binary))));
      const decimal = /[0-9]/;
      const firstDecimal = /[1-9]/;
      const intDecimalDigits = seq(firstDecimal, repeat(decimal), repeat(seq(separator, repeat1(decimal))));
      const floatDecimalDigits = seq(repeat1(decimal), repeat(seq(separator, repeat1(decimal))));
      const hex = /[0-9a-fA-F]/;
      const hexDigits = seq(repeat1(hex), repeat(seq(separator, repeat1(hex))));
      const octal = /[0-7]/;
      const octalDigits = seq('0', repeat(octal), repeat(seq(separator, repeat1(octal))));
      const hexExponent = seq(/[pP]/, optional(sign), floatDecimalDigits);
      const decimalExponent = seq(/[eE]/, optional(sign), floatDecimalDigits);
      const intSuffix = /(ll|LL)[uU]?|[uU](ll|LL)?|[uU][lL]?|[uU][zZ]?|[lL][uU]?|[zZ][uU]?/;
      const floatSuffix = /([fF](16|32|64|128)?)|[lL]|(bf16|BF16)/;

      return token(seq(
        optional(sign),
        choice(
          seq(
            choice(
              seq(choice('0b', '0B'), binaryDigits),
              intDecimalDigits,
              seq(choice('0x', '0X'), hexDigits),
              octalDigits,
            ),
            optional(intSuffix),
          ),
          seq(
            choice(
              seq(floatDecimalDigits, decimalExponent),
              seq(floatDecimalDigits, '.', optional(floatDecimalDigits), optional(decimalExponent)),
              seq('.', floatDecimalDigits, optional(decimalExponent)),
              seq(
                choice('0x', '0X'),
                choice(
                  hexDigits,
                  seq(hexDigits, '.', optional(hexDigits)),
                  seq('.', hexDigits)),
                hexExponent,
              ),
            ),
            optional(floatSuffix),
          ),
        ),
      ));
    },

    literal_suffix: _ => token.immediate(/[a-zA-Z_]\w*/),

    user_defined_literal: $ => seq(
      choice(
        $.number_literal,
        $.char_literal,
        $._string,
      ),
      $.literal_suffix,
    ),

    _namespace_identifier: $ => alias($.identifier, $.namespace_identifier),
  },
});

/**
 * Creates a rule to optionally match one or more of the rules separated by a comma
 *
 * @param {Rule} rule
 *
 * @returns {ChoiceRule}
 */
function commaSep(rule) {
  return optional(commaSep1(rule));
}

/**
 * Creates a rule to match one or more of the rules separated by a comma
 *
 * @param {Rule} rule
 *
 * @returns {SeqRule}
 */
function commaSep1(rule) {
  return seq(rule, repeat(seq(',', rule)));
}
