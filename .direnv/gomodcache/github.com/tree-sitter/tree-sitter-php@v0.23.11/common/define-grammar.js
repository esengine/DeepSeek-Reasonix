/**
 * @author Josh Vera <vera@github.com>
 * @author Max Brunsfeld <maxbrunsfeld@gmail.com>
 * @author Amaan Qureshi <amaanq12@gmail.com>
 * @author Caleb White <cdwhite3@pm.me>
 * @author Christian Frøystad <christian@xist.no>
 * @license MIT
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

const PREC = {
  COMMA: -1,
  CAST: -1,
  LOGICAL_OR_2: 1,
  LOGICAL_XOR: 2,
  LOGICAL_AND_2: 3,
  ASSIGNMENT: 4,
  TERNARY: 5,
  NULL_COALESCE: 6,
  LOGICAL_OR_1: 7,
  LOGICAL_AND_1: 8,
  BITWISE_OR: 9,
  BITWISE_XOR: 10,
  BITWISE_AND: 11,
  EQUALITY: 12,
  INEQUALITY: 13,
  CONCAT: 14,
  SHIFT: 15,
  PLUS: 16,
  TIMES: 17,
  EXPONENTIAL: 18,
  NEG: 19,
  INSTANCEOF: 20,
  INC: 21,
  SCOPE: 22,
  NEW: 23,
  CALL: 24,
  MEMBER: 25,
  DEREF: 26,
};

module.exports = function defineGrammar(dialect) {
  if (dialect !== 'php' && dialect !== 'php_only') {
    throw new Error(`Unknown dialect ${dialect}`);
  }

  return grammar({
    name: dialect,

    conflicts: $ => [
      [$._array_destructing, $.array_creation_expression],
      [$._array_destructing_element, $.array_element_initializer],
      [$.primary_expression, $._array_destructing_element],

      [$.type, $.union_type, $.intersection_type, $.disjunctive_normal_form_type],
      [$.union_type, $.disjunctive_normal_form_type],
      [$.intersection_type],
      [$.if_statement],

      [$.namespace_name],
      [$.heredoc_body],
    ],

    externals: $ => [
      $._automatic_semicolon,
      $.encapsed_string_chars,
      $.encapsed_string_chars_after_variable,
      $.execution_string_chars,
      $.execution_string_chars_after_variable,
      $.encapsed_string_chars_heredoc,
      $.encapsed_string_chars_after_variable_heredoc,
      $._eof,
      $.heredoc_start,
      $.heredoc_end,
      $.nowdoc_string,
      $.sentinel_error, // Unused token used to indicate error recovery mode
    ],

    extras: $ => {
      const extras = [
        $.comment,
        /[\s\u00A0\u200B\u2060\uFEFF]/,
      ];

      if (dialect === 'php') {
        extras.push($.text_interpolation);
      }

      return extras;
    },

    inline: $ => [
      $._variable,
      $._namespace_use_type,
    ],

    supertypes: $ => [
      $.statement,
      $.expression,
      $.primary_expression,
      $.type,
      $.literal,
    ],

    word: $ => $.name,

    rules: {
      program: $ => {
        if (dialect === 'php') {
          return seq(
            optional($.text),
            optional(seq(
              $.php_tag,
              repeat($.statement),
            )),
          );
        }

        return seq(
          optional($.php_tag),
          repeat($.statement),
          optional('?>'),
        );
      },

      php_tag: _ => /<\?([pP][hH][pP]|=)?/,

      text_interpolation: $ => seq(
        '?>',
        optional($.text),
        choice($.php_tag, $._eof),
      ),

      text: _ => repeat1(choice(
        token(prec(-1, /</)),
        token(prec(1, /[^\s<][^<]*/)),
      )),

      statement: $ => choice(
        $.empty_statement,
        $.compound_statement,
        $.named_label_statement,
        $.expression_statement,
        $.if_statement,
        $.switch_statement,
        $.while_statement,
        $.do_statement,
        $.for_statement,
        $.foreach_statement,
        $.goto_statement,
        $.continue_statement,
        $.break_statement,
        $.return_statement,
        $.try_statement,
        $.declare_statement,
        $.echo_statement,
        $.exit_statement,
        $.unset_statement,
        $.const_declaration,
        $.function_definition,
        $.class_declaration,
        $.interface_declaration,
        $.trait_declaration,
        $.enum_declaration,
        $.namespace_definition,
        $.namespace_use_declaration,
        $.global_declaration,
        $.function_static_declaration,
      ),

      empty_statement: _ => prec(-1, ';'),

      reference_modifier: _ => '&',

      function_static_declaration: $ => seq(
        keyword('static'),
        commaSep1($.static_variable_declaration),
        $._semicolon,
      ),

      static_variable_declaration: $ => seq(
        field('name', $.variable_name),
        optional(seq(
          '=',
          field('value', $.expression),
        )),
      ),

      global_declaration: $ => seq(
        keyword('global'),
        commaSep1($._simple_variable),
        $._semicolon,
      ),

      namespace_definition: $ => seq(
        keyword('namespace'),
        choice(
          seq(field('name', $.namespace_name), $._semicolon),
          seq(
            field('name', optional($.namespace_name)),
            field('body', $.compound_statement),
          ),
        ),
      ),

      namespace_use_declaration: $ => seq(
        keyword('use'),
        choice(
          commaSep1($.namespace_use_clause),
          $._namespace_use_group,
        ),
        $._semicolon,
      ),

      namespace_use_clause: $ => seq(
        field('type', optional($._namespace_use_type)),
        $._name,
        optional(seq(keyword('as'), field('alias', $.name))),
      ),

      _namespace_use_type: _ => choice(keyword('function'), keyword('const')),

      qualified_name: $ => seq(
        field('prefix', seq(optional($.namespace_name), '\\')),
        $.name,
      ),

      _name: $ => choice($._identifier, $.qualified_name),

      namespace_name: $ => seq(optional('\\'), $.name, repeat(seq('\\', $.name))),

      _namespace_use_group: $ => seq(
        field('type', optional($._namespace_use_type)),
        $.namespace_name,
        '\\',
        field('body', $.namespace_use_group),
      ),

      namespace_use_group: $ => seq('{', commaSep1($.namespace_use_clause), '}'),

      trait_declaration: $ => seq(
        optional(field('attributes', $.attribute_list)),
        keyword('trait'),
        field('name', $.name),
        field('body', $.declaration_list),
      ),

      interface_declaration: $ => seq(
        optional(field('attributes', $.attribute_list)),
        keyword('interface'),
        field('name', $.name),
        optional($.base_clause),
        field('body', $.declaration_list),
      ),

      base_clause: $ => seq(
        keyword('extends'),
        commaSep1($._name),
      ),

      enum_declaration: $ => prec.right(seq(
        optional(field('attributes', $.attribute_list)),
        keyword('enum'),
        field('name', $.name),
        optional(seq(':', alias(choice('string', 'int'), $.primitive_type))),
        optional($.class_interface_clause),
        field('body', $.enum_declaration_list),
      )),

      enum_declaration_list: $ => seq('{', repeat($._enum_member_declaration), '}'),

      _enum_member_declaration: $ => choice(
        $.enum_case,
        $.method_declaration,
        $.use_declaration,
      ),

      enum_case: $ => seq(
        optional(field('attributes', $.attribute_list)),
        keyword('case'),
        field('name', $.name),
        optional(seq('=', field('value', $.expression))),
        $._semicolon,
      ),

      class_declaration: $ => prec.right(seq(
        optional(field('attributes', $.attribute_list)),
        repeat($._modifier),
        keyword('class'),
        field('name', $.name),
        optional($.base_clause),
        optional($.class_interface_clause),
        field('body', $.declaration_list),
      )),

      declaration_list: $ => seq('{', repeat($._member_declaration), '}'),

      final_modifier: _ => keyword('final'),
      abstract_modifier: _ => keyword('abstract'),
      readonly_modifier: _ => keyword('readonly'),

      class_interface_clause: $ => seq(
        keyword('implements'),
        commaSep1($._name),
      ),

      _member_declaration: $ => choice(
        alias($._class_const_declaration, $.const_declaration),
        $.property_declaration,
        $.method_declaration,
        $.use_declaration,
      ),

      const_declaration: $ => $._const_declaration,

      _class_const_declaration: $ => seq(
        optional(field('attributes', $.attribute_list)),
        optional($.final_modifier),
        $._const_declaration,
      ),

      _const_declaration: $ => seq(
        repeat($._modifier),
        keyword('const'),
        optional(field('type', $.type)),
        commaSep1($.const_element),
        $._semicolon,
      ),

      property_declaration: $ => seq(
        optional(field('attributes', $.attribute_list)),
        repeat1($._modifier),
        optional(field('type', $.type)),
        commaSep1($.property_element),
        choice(
          $._semicolon,
          $.property_hook_list,
        ),
      ),

      _modifier: $ => prec.left(choice(
        $.var_modifier,
        $.visibility_modifier,
        $.static_modifier,
        $.final_modifier,
        $.abstract_modifier,
        $.readonly_modifier,
      )),

      property_element: $ => seq(
        field('name', $.variable_name),
        optional(seq('=', field('default_value', $.expression))),
      ),

      property_hook_list: $ => seq('{', repeat($.property_hook), '}'),

      property_hook: $ => seq(
        optional(field('attributes', $.attribute_list)),
        optional(field('final', $.final_modifier)),
        optional(field('reference_modifier', $.reference_modifier)),
        $.name,
        optional(field('parameters', $.formal_parameters)),
        $._property_hook_body,
      ),

      _property_hook_body: $ => choice(
        seq('=>', field('body', $.expression), $._semicolon),
        field('body', $.compound_statement),
        $._semicolon,
      ),

      method_declaration: $ => seq(
        optional(field('attributes', $.attribute_list)),
        repeat($._modifier),
        $._function_definition_header,
        choice(
          field('body', $.compound_statement),
          $._semicolon,
        ),
      ),

      var_modifier: _ => keyword('var', false),
      static_modifier: _ => keyword('static'),

      use_declaration: $ => seq(
        keyword('use'),
        commaSep1($._name),
        choice($.use_list, $._semicolon),
      ),

      use_list: $ => seq(
        '{',
        repeat(seq(
          choice(
            $.use_instead_of_clause,
            $.use_as_clause,
          ),
          $._semicolon,
        )),
        '}',
      ),

      use_instead_of_clause: $ => prec.left(seq(
        $.class_constant_access_expression,
        keyword('insteadof'),
        $.name,
      )),

      use_as_clause: $ => seq(
        choice($.class_constant_access_expression, $.name),
        keyword('as'),
        choice(
          seq(
            optional($.visibility_modifier),
            $.name,
          ),
          seq(
            $.visibility_modifier,
            optional($.name),
          ),
        ),
      ),

      visibility_modifier: $ => seq(
        choice(
          keyword('public'),
          keyword('protected'),
          keyword('private'),
        ),
        optional(seq(
          token.immediate('('),
          alias($.name, $.operation),
          token.immediate(')'),
        )),
      ),

      function_definition: $ => seq(
        optional(field('attributes', $.attribute_list)),
        $._function_definition_header,
        field('body', $.compound_statement),
      ),

      _function_definition_header: $ => seq(
        keyword('function'),
        optional($.reference_modifier),
        field('name', $._identifier),
        field('parameters', $.formal_parameters),
        optional($._return_type),
      ),

      anonymous_function: $ => seq(
        $._anonymous_function_header,
        field('body', $.compound_statement),
      ),

      anonymous_function_use_clause: $ => seq(
        keyword('use'),
        '(',
        commaSep1(choice($.by_ref, $.variable_name)),
        optional(','),
        ')',
      ),

      _anonymous_function_header: $ => seq(
        optional(field('attributes', $.attribute_list)),
        optional(field('static_modifier', $.static_modifier)),
        keyword('function'),
        optional(field('reference_modifier', $.reference_modifier)),
        field('parameters', $.formal_parameters),
        optional($.anonymous_function_use_clause),
        optional($._return_type),
      ),

      _arrow_function_header: $ => seq(
        optional(field('attributes', $.attribute_list)),
        optional(field('static_modifier', $.static_modifier)),
        keyword('fn'),
        optional(field('reference_modifier', $.reference_modifier)),
        field('parameters', $.formal_parameters),
        optional($._return_type),
      ),

      arrow_function: $ => seq(
        $._arrow_function_header,
        '=>',
        field('body', $.expression),
      ),

      formal_parameters: $ => seq(
        '(',
        commaSep(choice(
          $.simple_parameter,
          $.variadic_parameter,
          $.property_promotion_parameter,
        )),
        optional(','),
        ')',
      ),

      property_promotion_parameter: $ => seq(
        optional(field('attributes', $.attribute_list)),
        field('visibility', $.visibility_modifier),
        field('readonly', optional($.readonly_modifier)),
        field('type', optional($.type)), // Note: callable is not a valid type here, but instead of complicating the parser, we defer this checking to any intelligence using the parser
        field('name', choice($.by_ref, $.variable_name)),
        optional(seq('=', field('default_value', $.expression))),
        optional($.property_hook_list),
      ),

      simple_parameter: $ => seq(
        optional(field('attributes', $.attribute_list)),
        field('type', optional($.type)),
        optional(field('reference_modifier', $.reference_modifier)),
        field('name', $.variable_name),
        optional(seq('=', field('default_value', $.expression))),
      ),

      variadic_parameter: $ => seq(
        optional(field('attributes', $.attribute_list)),
        field('type', optional($.type)),
        optional(field('reference_modifier', $.reference_modifier)),
        '...',
        field('name', $.variable_name),
      ),

      type: $ => choice(
        $._types,
        $.union_type,
        $.intersection_type,
        $.disjunctive_normal_form_type,
      ),

      _types: $ => choice(
        $.optional_type,
        $.named_type,
        $.primitive_type,
      ),

      named_type: $ => choice($.name, $.qualified_name),

      optional_type: $ => seq(
        '?',
        choice(
          $.named_type,
          $.primitive_type,
        ),
      ),

      bottom_type: _ => 'never',

      union_type: $ => pipeSep1($._types),

      intersection_type: $ => ampSep1($._types),

      disjunctive_normal_form_type: $ => prec.dynamic(-1, pipeSep1(choice(
        seq('(', $.intersection_type, ')'),
        $._types,
      ))),

      primitive_type: _ => choice(
        'array',
        keyword('callable'), // not legal in property types
        'iterable',
        'bool',
        'float',
        'int',
        'string',
        'void',
        'mixed',
        'false',
        'null',
        'true',
      ),

      cast_type: _ => choice(
        keyword('array', false),
        keyword('binary', false),
        keyword('bool', false),
        keyword('boolean', false),
        keyword('double', false),
        keyword('int', false),
        keyword('integer', false),
        keyword('float', false),
        keyword('object', false),
        keyword('real', false),
        keyword('string', false),
        keyword('unset', false),
      ),

      _return_type: $ => seq(':', field('return_type', choice($.type, $.bottom_type))),

      const_element: $ => seq($._identifier, '=', $.expression),

      echo_statement: $ => seq(keyword('echo'), $._expressions, $._semicolon),

      exit_statement: $ => seq(
        keyword('exit'),
        optional(seq('(', optional($.expression), ')')),
        $._semicolon,
      ),

      unset_statement: $ => seq(
        'unset',
        '(',
        commaSep1($._variable),
        optional(','),
        ')',
        $._semicolon,
      ),

      declare_statement: $ => seq(
        keyword('declare'),
        '(',
        $.declare_directive,
        ')',
        choice(
          $.statement,
          $._semicolon,
          seq(
            ':',
            repeat($.statement),
            keyword('enddeclare'),
            $._semicolon,
          ),
        ),
      ),

      declare_directive: $ => seq(
        choice('ticks', 'encoding', 'strict_types'),
        '=',
        $.literal,
      ),

      literal: $ => choice(
        $.integer,
        $.float,
        $._string,
        $.boolean,
        $.null,
      ),

      float: _ => /\d*(_\d+)*((\.\d*(_\d+)*)?([eE][\+-]?\d+(_\d+)*)|(\.\d*(_\d+)*)([eE][\+-]?\d+(_\d+)*)?)/,

      try_statement: $ => seq(
        keyword('try'),
        field('body', $.compound_statement),
        repeat1(choice($.catch_clause, $.finally_clause)),
      ),

      catch_clause: $ => seq(
        keyword('catch'),
        '(',
        field('type', $.type_list),
        optional(field('name', $.variable_name)),
        ')',
        field('body', $.compound_statement),
      ),

      type_list: $ => pipeSep1($.named_type),

      finally_clause: $ => seq(
        keyword('finally'),
        field('body', $.compound_statement),
      ),

      goto_statement: $ => seq(
        keyword('goto'), $.name, $._semicolon,
      ),

      continue_statement: $ => seq(
        keyword('continue'), optional($.expression), $._semicolon,
      ),

      break_statement: $ => seq(
        keyword('break'), optional($.expression), $._semicolon,
      ),

      integer: _ => {
        const decimal = /[1-9]\d*(_\d+)*/;
        const octal = /0[oO]?[0-7]*(_[0-7]+)*/;
        const hex = /0[xX][0-9a-fA-F]+(_[0-9a-fA-F]+)*/;
        const binary = /0[bB][01]+(_[01]+)*/;
        return token(choice(
          decimal,
          octal,
          hex,
          binary,
        ));
      },

      return_statement: $ => seq(
        keyword('return'), optional($.expression), $._semicolon,
      ),

      throw_expression: $ => seq(
        keyword('throw'),
        $.expression,
      ),

      while_statement: $ => seq(
        keyword('while'),
        field('condition', $.parenthesized_expression),
        choice(
          field('body', $.statement),
          seq(
            field('body', $.colon_block),
            keyword('endwhile'),
            $._semicolon,
          ),
        ),
      ),

      do_statement: $ => seq(
        keyword('do'),
        field('body', $.statement),
        keyword('while'),
        field('condition', $.parenthesized_expression),
        $._semicolon,
      ),

      for_statement: $ => seq(
        keyword('for'),
        '(',
        field('initialize', optional($._expressions)),
        ';',
        field('condition', optional($._expressions)),
        ';',
        field('update', optional($._expressions)),
        ')',
        choice(
          $._semicolon,
          field('body', $.statement),
          seq(
            ':',
            field('body', repeat($.statement)),
            keyword('endfor'),
            $._semicolon,
          ),
        ),
      ),

      _expressions: $ => choice(
        $.expression,
        $.sequence_expression,
      ),

      sequence_expression: $ => prec(PREC.COMMA, seq(
        $.expression, ',', choice($.sequence_expression, $.expression)),
      ),

      foreach_statement: $ => seq(
        keyword('foreach'),
        '(',
        $.expression,
        keyword('as'),
        choice(
          alias($.foreach_pair, $.pair),
          $._foreach_value,
        ),
        ')',
        choice(
          $._semicolon,
          field('body', $.statement),
          seq(
            field('body', $.colon_block),
            keyword('endforeach'),
            $._semicolon,
          ),
        ),
      ),

      foreach_pair: $ => seq($.expression, '=>', $._foreach_value),

      _foreach_value: $ => choice(
        $.by_ref,
        $.expression,
        $.list_literal,
      ),

      if_statement: $ => seq(
        keyword('if'),
        field('condition', $.parenthesized_expression),
        choice(
          seq(
            field('body', $.statement),
            repeat(field('alternative', $.else_if_clause)),
            optional(field('alternative', $.else_clause)),
          ),
          seq(
            field('body', $.colon_block),
            repeat(field('alternative', alias($.else_if_clause_2, $.else_if_clause))),
            optional(field('alternative', alias($.else_clause_2, $.else_clause))),
            keyword('endif'),
            $._semicolon,
          ),
        ),
      ),

      colon_block: $ => seq(
        ':',
        repeat($.statement),
      ),

      else_if_clause: $ => seq(
        keyword('elseif'),
        field('condition', $.parenthesized_expression),
        field('body', $.statement),
      ),

      else_clause: $ => seq(
        keyword('else'),
        field('body', $.statement),
      ),

      else_if_clause_2: $ => seq(
        keyword('elseif'),
        field('condition', $.parenthesized_expression),
        field('body', $.colon_block),
      ),

      else_clause_2: $ => seq(
        keyword('else'),
        field('body', $.colon_block),
      ),

      match_expression: $ => seq(
        keyword('match'),
        field('condition', $.parenthesized_expression),
        field('body', $.match_block),
      ),

      match_block: $ => prec.left(
        seq(
          '{',
          commaSep(
            choice(
              $.match_conditional_expression,
              $.match_default_expression,
            ),
          ),
          optional(','),
          '}',
        ),
      ),

      match_condition_list: $ => seq(commaSep1($.expression), optional(',')),

      match_conditional_expression: $ => seq(
        field('conditional_expressions', $.match_condition_list),
        '=>',
        field('return_expression', $.expression),
      ),

      match_default_expression: $ => seq(
        keyword('default'),
        '=>',
        field('return_expression', $.expression),
      ),

      switch_statement: $ => seq(
        keyword('switch'),
        field('condition', $.parenthesized_expression),
        field('body', $.switch_block),
      ),

      switch_block: $ => choice(
        seq(
          '{',
          repeat(choice($.case_statement, $.default_statement)),
          '}',
        ),
        seq(
          ':',
          repeat(choice($.case_statement, $.default_statement)),
          keyword('endswitch'),
          $._semicolon,
        ),
      ),

      case_statement: $ => seq(
        keyword('case'),
        field('value', $.expression),
        choice(':', ';'),
        repeat($.statement),
      ),

      default_statement: $ => seq(
        keyword('default'),
        choice(':', ';'),
        repeat($.statement),
      ),

      compound_statement: $ => seq('{', repeat($.statement), '}'),

      named_label_statement: $ => seq($.name, ':'),

      expression_statement: $ => seq($.expression, $._semicolon),

      expression: $ => choice(
        $.conditional_expression,
        $.match_expression,
        $.augmented_assignment_expression,
        $.assignment_expression,
        $.reference_assignment_expression,
        $.yield_expression,
        $._unary_expression,
        $.error_suppression_expression,
        $.binary_expression,
        $.include_expression,
        $.include_once_expression,
        $.require_expression,
        $.require_once_expression,
      ),

      _unary_expression: $ => choice(
        $.clone_expression,
        $.primary_expression,
        $.unary_op_expression,
        $.cast_expression,
      ),

      unary_op_expression: $ => prec.left(PREC.NEG, seq(
        field('operator', choice('+', '-', '~', '!')),
        field('argument', $.expression),
      )),

      error_suppression_expression: $ => prec(PREC.INC, seq('@', $.expression)),

      clone_expression: $ => seq(keyword('clone'), $.primary_expression),

      primary_expression: $ => choice(
        $._variable,
        $.literal,
        $.class_constant_access_expression,
        $.qualified_name,
        $.name,
        $.array_creation_expression,
        $.print_intrinsic,
        $.anonymous_function,
        $.arrow_function,
        $.object_creation_expression,
        $.update_expression,
        $.shell_command_expression,
        $.parenthesized_expression,
        $.throw_expression,
        $.arrow_function,
      ),

      parenthesized_expression: $ => seq('(', $.expression, ')'),

      class_constant_access_expression: $ => seq(
        $._scope_resolution_qualifier,
        '::',
        choice(
          $._identifier,
          seq('{', alias($.expression, $.name), '}'),
        ),
      ),

      print_intrinsic: $ => seq(
        keyword('print'), $.expression,
      ),

      object_creation_expression: $ => choice(
        $._new_dereferencable_expression,
        $._new_non_dereferencable_expression,
      ),

      _new_non_dereferencable_expression: $ => prec.right(PREC.NEW, seq(
        keyword('new'),
        $._class_name_reference,
      )),

      _new_dereferencable_expression: $ => prec.right(PREC.NEW, seq(
        keyword('new'),
        choice(
          seq($._class_name_reference, $.arguments),
          $.anonymous_class,
        ),
      )),

      _class_name_reference: $ => choice(
        $._name,
        $._new_variable,
        $.parenthesized_expression,
      ),

      anonymous_class: $ => prec.right(seq(
        optional(field('attributes', $.attribute_list)),
        repeat($._modifier),
        keyword('class'),
        optional($.arguments),
        optional($.base_clause),
        optional($.class_interface_clause),
        field('body', $.declaration_list),
      )),

      update_expression: $ => {
        const argument = field('argument', $._variable);
        const operator = field('operator', choice('--', '++'));
        return prec.left(PREC.INC, choice(
          seq(operator, argument),
          seq(argument, operator),
        ));
      },

      cast_expression: $ => prec(PREC.CAST, seq(
        '(',
        field('type', $.cast_type),
        ')',
        field('value', choice(
          $._unary_expression,
          $.include_expression,
          $.include_once_expression,
          $.error_suppression_expression,
        )),
      )),

      cast_variable: $ => prec(PREC.CAST, seq(
        '(', field('type', $.cast_type), ')',
        field('value', $._variable),
      )),

      assignment_expression: $ => prec.right(PREC.ASSIGNMENT, seq(
        field('left', choice(
          $._variable,
          $.list_literal,
        )),
        '=',
        field('right', $.expression),
      )),

      reference_assignment_expression: $ => prec.right(PREC.ASSIGNMENT, seq(
        field('left', choice(
          $._variable,
          $.list_literal,
        )),
        '=',
        '&',
        field('right', $.expression),
      )),

      conditional_expression: $ => prec.left(PREC.TERNARY, seq( // TODO: Ternay is non-assossiative after PHP 8
        field('condition', $.expression),
        '?',
        field('body', optional($.expression)),
        ':',
        field('alternative', $.expression),
      )),

      augmented_assignment_expression: $ => prec.right(PREC.ASSIGNMENT, seq(
        field('left', $._variable),
        field('operator', choice(
          '**=',
          '*=',
          '/=',
          '%=',
          '+=',
          '-=',
          '.=',
          '<<=',
          '>>=',
          '&=',
          '^=',
          '|=',
          '??=',
        )),
        field('right', $.expression),
      )),

      _variable: $ => choice(
        alias($.cast_variable, $.cast_expression),
        $._new_variable,
        $._callable_variable,
        $.scoped_property_access_expression,
        $.member_access_expression,
        $.nullsafe_member_access_expression,
      ),

      _variable_member_access_expression: $ => prec(PREC.MEMBER, seq(
        field('object', $._new_variable),
        '->',
        $._member_name,
      )),

      member_access_expression: $ => prec(PREC.MEMBER, seq(
        field('object', $._dereferencable_expression),
        '->',
        $._member_name,
      )),

      _variable_nullsafe_member_access_expression: $ => prec(PREC.MEMBER, seq(
        field('object', $._new_variable),
        '?->',
        $._member_name,
      )),

      nullsafe_member_access_expression: $ => prec(PREC.MEMBER, seq(
        field('object', $._dereferencable_expression),
        '?->',
        $._member_name,
      )),

      _variable_scoped_property_access_expression: $ => prec(PREC.MEMBER, seq(
        field('scope', choice($._name, $._new_variable)),
        '::',
        field('name', $._simple_variable),
      )),

      scoped_property_access_expression: $ => prec(PREC.MEMBER, seq(
        field('scope', $._scope_resolution_qualifier),
        '::',
        field('name', $._simple_variable),
      )),

      list_literal: $ => choice($._list_destructing, $._array_destructing),

      _list_destructing: $ => seq(
        keyword('list'),
        '(',
        commaSep1(optional(
          choice(
            alias($._list_destructing, $.list_literal),
            $._variable,
            $.by_ref,
            seq(
              $.expression,
              '=>',
              choice(
                alias($._list_destructing, $.list_literal),
                $._variable,
                $.by_ref,
              ),
            ),
          ),
        )),
        ')',
      ),

      _array_destructing: $ => seq(
        '[',
        commaSep1(optional($._array_destructing_element)),
        ']',
      ),

      _array_destructing_element: $ => choice(
        choice(
          alias($._array_destructing, $.list_literal),
          $._variable,
          $.by_ref,
        ),
        seq(
          $.expression,
          '=>',
          choice(
            alias($._array_destructing, $.list_literal),
            $._variable,
            $.by_ref,
          ),
        ),
      ),

      function_call_expression: $ => prec(PREC.CALL, seq(
        field('function', choice($._name, $._callable_expression)),
        field('arguments', $.arguments),
      )),

      _callable_expression: $ => choice(
        $._callable_variable,
        $.parenthesized_expression,
        $._dereferencable_scalar,
        alias($._new_dereferencable_expression, $.object_creation_expression),
      ),

      scoped_call_expression: $ => prec(PREC.CALL, seq(
        field('scope', $._scope_resolution_qualifier),
        '::',
        $._member_name,
        field('arguments', $.arguments),
      )),

      _scope_resolution_qualifier: $ => choice(
        $.relative_scope,
        $._name,
        $._dereferencable_expression,
      ),

      relative_scope: _ => prec(PREC.SCOPE, choice(
        'self',
        'parent',
        keyword('static'),
      )),

      variadic_placeholder: _ => '...',

      arguments: $ => seq(
        '(',
        optional(choice(
          seq(commaSep1($.argument), optional(',')),
          $.variadic_placeholder,
        )),
        ')',
      ),

      argument: $ => seq(
        optional($._argument_name),
        optional(field('reference_modifier', $.reference_modifier)),
        choice(
          alias($._reserved_identifier, $.name),
          $.variadic_unpacking,
          $.expression,
        ),
      ),

      _argument_name: $ => seq(
        field('name', alias(
          choice(
            $.name,
            keyword('array', false),
            keyword('fn', false),
            keyword('function', false),
            keyword('match', false),
            keyword('namespace', false),
            keyword('null', false),
            keyword('static', false),
            keyword('throw', false),
            'parent',
            'self',
            /true|false/i,
          ),
          $.name,
        )),
        ':',
      ),

      member_call_expression: $ => prec(PREC.CALL, seq(
        field('object', $._dereferencable_expression),
        '->',
        $._member_name,
        field('arguments', $.arguments),
      )),

      nullsafe_member_call_expression: $ => prec(PREC.CALL, seq(
        field('object', $._dereferencable_expression),
        '?->',
        $._member_name,
        field('arguments', $.arguments),
      )),

      variadic_unpacking: $ => seq('...', $.expression),

      _member_name: $ => choice(
        field('name', choice(
          $._identifier,
          $._simple_variable,
        )),
        seq('{', field('name', $.expression), '}'),
      ),

      _variable_subscript_expression: $ => seq(
        $._new_variable,
        seq('[', optional($.expression), ']'),
      ),

      _dereferencable_subscript_expression: $ => seq(
        $._dereferencable_expression,
        seq('[', optional($.expression), ']'),
      ),

      _dereferencable_expression: $ => prec(PREC.DEREF, choice(
        $._variable,
        alias($._new_dereferencable_expression, $.object_creation_expression),
        $.class_constant_access_expression,
        $.parenthesized_expression,
        $._dereferencable_scalar,
        $._name,
      )),

      _dereferencable_scalar: $ => prec(PREC.DEREF, choice(
        $.array_creation_expression,
        $._string,
      )),

      array_creation_expression: $ => choice(
        seq(keyword('array'), '(', commaSep($.array_element_initializer), optional(','), ')'),
        seq('[', commaSep($.array_element_initializer), optional(','), ']'),
      ),

      attribute_group: $ => seq(
        '#[',
        commaSep1($.attribute),
        optional(','),
        ']',
      ),

      attribute_list: $ => repeat1($.attribute_group),

      attribute: $ => seq(
        $._name,
        optional(field('parameters', $.arguments)),
      ),

      _complex_string_part: $ => seq('{', $.expression, '}'),

      _simple_string_member_access_expression: $ => prec(PREC.MEMBER, seq(
        field('object', $.variable_name),
        '->',
        field('name', $.name),
      )),

      _simple_string_subscript_unary_expression: $ => prec.left(seq('-', $.integer)),

      _simple_string_array_access_argument: $ => choice(
        $.integer,
        alias($._simple_string_subscript_unary_expression, $.unary_op_expression),
        $.name,
        $.variable_name,
      ),

      _simple_string_subscript_expression: $ => prec(PREC.DEREF, seq(
        $.variable_name,
        seq('[', $._simple_string_array_access_argument, ']'),
      )),

      _simple_string_part: $ => choice(
        alias($._simple_string_member_access_expression, $.member_access_expression),
        $._simple_variable,
        alias($._simple_string_subscript_expression, $.subscript_expression),
      ),

      // Note: remember to also update the is_escapable_sequence method in the
      // external scanner whenever changing these rules
      escape_sequence: _ => token.immediate(seq(
        '\\',
        choice(
          'n',
          'r',
          't',
          'v',
          'e',
          'f',
          '\\',
          /\$/,
          '"',
          '`',
          /[0-7]{1,3}/,
          /x[0-9A-Fa-f]{1,2}/,
          /u\{[0-9A-Fa-f]+\}/,
        ),
      )),

      _interpolated_string_body: $ => repeat1(
        choice(
          $.escape_sequence,
          seq($.variable_name, alias($.encapsed_string_chars_after_variable, $.string_content)),
          alias($.encapsed_string_chars, $.string_content),
          $._simple_string_part,
          $._complex_string_part,
          alias('\\u', $.string_content),
        ),
      ),

      _interpolated_string_body_heredoc: $ => repeat1(
        choice(
          $.escape_sequence,
          seq(
            $.variable_name,
            alias($.encapsed_string_chars_after_variable_heredoc, $.string_content),
          ),
          alias($.encapsed_string_chars_heredoc, $.string_content),
          $._simple_string_part,
          $._complex_string_part,
          alias('\\u', $.string_content),
        ),
      ),

      encapsed_string: $ => prec.right(seq(
        choice(/[bB]"/, '"'),
        optional($._interpolated_string_body),
        '"',
      )),

      string: $ => seq(
        choice(/[bB]'/, '\''),
        repeat(choice(
          alias(token(choice('\\\\', '\\\'')), $.escape_sequence),
          $.string_content,
        )),
        '\'',
      ),

      string_content: _ => prec.right(repeat1(token.immediate(prec(1, /\\?[^'\\]+/)))),

      heredoc_body: $ => seq(
        $._new_line,
        repeat1(prec.right(seq(
          optional($._new_line),
          $._interpolated_string_body_heredoc,
        ))),
      ),

      heredoc: $ => seq(
        token('<<<'),
        optional('"'),
        field('identifier', $.heredoc_start),
        optional(token.immediate('"')),
        choice(
          seq(
            field('value', $.heredoc_body),
            $._new_line,
          ),
          field('value', optional($.heredoc_body)),
        ),
        field('end_tag', $.heredoc_end),
      ),

      _new_line: _ => /\r?\n|\r/,

      nowdoc_body: $ => seq(
        $._new_line,
        repeat1($.nowdoc_string),
      ),

      nowdoc: $ => seq(
        token('<<<'),
        '\'',
        field('identifier', $.heredoc_start),
        token.immediate('\''),
        choice(
          seq(
            field('value', $.nowdoc_body),
            $._new_line,
          ),
          field('value', optional($.nowdoc_body)),
        ),
        field('end_tag', $.heredoc_end),
      ),

      _interpolated_execution_operator_body: $ => repeat1(
        choice(
          $.escape_sequence,
          seq($.variable_name, alias($.execution_string_chars_after_variable, $.string_content)),
          alias($.execution_string_chars, $.string_content),
          $._simple_string_part,
          $._complex_string_part,
          alias('\\u', $.string_content),
        ),
      ),

      shell_command_expression: $ => seq(
        '`',
        optional($._interpolated_execution_operator_body),
        '`',
      ),

      boolean: _ => /true|false/i,

      null: _ => keyword('null', false),

      _string: $ => choice($.encapsed_string, $.string, $.heredoc, $.nowdoc),

      dynamic_variable_name: $ => choice(
        seq('$', $._simple_variable),
        seq('$', '{', $.expression, '}'),
      ),

      _simple_variable: $ => choice($.variable_name, $.dynamic_variable_name),

      _new_variable: $ => prec(1, choice(
        $._simple_variable,
        alias($._variable_subscript_expression, $.subscript_expression),
        alias($._variable_member_access_expression, $.member_access_expression),
        alias($._variable_nullsafe_member_access_expression, $.nullsafe_member_access_expression),
        alias($._variable_scoped_property_access_expression, $.scoped_property_access_expression),
      )),

      _callable_variable: $ => choice(
        $._simple_variable,
        alias($._dereferencable_subscript_expression, $.subscript_expression),
        $.member_call_expression,
        $.nullsafe_member_call_expression,
        $.function_call_expression,
        $.scoped_call_expression,
      ),

      variable_name: $ => seq('$', $.name),

      by_ref: $ => seq('&', $._variable),

      yield_expression: $ => prec.right(seq(
        keyword('yield'),
        optional(choice(
          $.array_element_initializer,
          seq(keyword('from'), $.expression),
        )),
      )),

      array_element_initializer: $ => prec.right(choice(
        choice($.by_ref, $.expression),
        seq($.expression, '=>', choice($.by_ref, $.expression)),
        $.variadic_unpacking,
      )),

      binary_expression: $ => choice(
        prec(PREC.INSTANCEOF, seq(
          field('left', $._unary_expression),
          field('operator', keyword('instanceof')),
          field('right', $._class_name_reference),
        )),
        prec.right(PREC.NULL_COALESCE, seq(
          field('left', $.expression),
          field('operator', '??'),
          field('right', $.expression),
        )),
        prec.right(PREC.EXPONENTIAL, seq(
          field('left', $.expression),
          field('operator', '**'),
          field('right', $.expression),
        )),
        ...[
          [keyword('and'), PREC.LOGICAL_AND_2],
          [keyword('or'), PREC.LOGICAL_OR_2],
          [keyword('xor'), PREC.LOGICAL_XOR],
          ['||', PREC.LOGICAL_OR_1],
          ['&&', PREC.LOGICAL_AND_1],
          ['|', PREC.BITWISE_OR],
          ['^', PREC.BITWISE_XOR],
          ['&', PREC.BITWISE_AND],
          ['==', PREC.EQUALITY],
          ['!=', PREC.EQUALITY],
          ['<>', PREC.EQUALITY],
          ['===', PREC.EQUALITY],
          ['!==', PREC.EQUALITY],
          ['<', PREC.INEQUALITY],
          ['>', PREC.INEQUALITY],
          ['<=', PREC.INEQUALITY],
          ['>=', PREC.INEQUALITY],
          ['<=>', PREC.EQUALITY],
          ['<<', PREC.SHIFT],
          ['>>', PREC.SHIFT],
          ['+', PREC.PLUS],
          ['-', PREC.PLUS],
          ['.', PREC.CONCAT],
          ['*', PREC.TIMES],
          ['/', PREC.TIMES],
          ['%', PREC.TIMES],
          // @ts-ignore
        ].map(([op, p]) => prec.left(p, seq(
          field('left', $.expression),
          // @ts-ignore
          field('operator', op),
          field('right', $.expression),
        ))),
      ),

      include_expression: $ => seq(
        keyword('include'),
        $.expression,
      ),

      include_once_expression: $ => seq(
        keyword('include_once'),
        $.expression,
      ),

      require_expression: $ => seq(
        keyword('require'),
        $.expression,
      ),

      require_once_expression: $ => seq(
        keyword('require_once'),
        $.expression,
      ),

      // Note that PHP officially only supports the following character regex
      // for identifiers: ^[a-zA-Z_\x80-\xff][a-zA-Z0-9_\x80-\xff]*$
      // However, there is a "bug" in how PHP parses multi-byte characters that allows
      // for a much larger range of characters to be used in identifiers.
      //
      // See: https://www.php.net/manual/en/language.variables.basics.php
      name: _ => {
        // We need to side step around the whitespace characters in the extras array.
        const range = String.raw`\u0080-\u009f\u00a1-\u200a\u200c-\u205f\u2061-\ufefe\uff00-\uffff`;
        return new RegExp(`[_a-zA-Z${range}][_a-zA-Z${range}\\d]*`);
      },

      _reserved_identifier: _ => choice(
        'self',
        'parent',
        keyword('static'),
      ),

      _identifier: $ => choice(
        $.name,
        alias($._reserved_identifier, $.name),
      ),

      comment: _ => token(choice(
        seq(
          choice('//', /#[^?\[?\r?\n]/),
          repeat(/[^?\r?\n]|\?[^>\r\n]/),
          optional(/\?\r?\n/),
        ),
        '#',
        seq(
          '/*',
          /[^*]*\*+([^/*][^*]*\*+)*/,
          '/',
        ),
      )),

      _semicolon: $ => choice($._automatic_semicolon, ';'),
    },
  });
};

/**
 * Creates a regex that matches the given word case-insensitively,
 * and will alias the regex to the word if aliasAsWord is true
 *
 * @param {string} word
 * @param {boolean} aliasAsWord
 *
 * @returns {RegExp|AliasRule}
 */
function keyword(word, aliasAsWord = true) {
  /** @type {RegExp|AliasRule} */
  let result = new RegExp(word, 'i');
  if (aliasAsWord) result = alias(result, word);
  return result;
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
 * Creates a rule to match one or more of the rules separated by a pipe
 *
 * @param {Rule} rule
 *
 * @returns {SeqRule}
 */
function pipeSep1(rule) {
  return seq(rule, repeat(seq('|', rule)));
}

/**
 * Creates a rule to  match one or more of the rules separated by an ampersand
 *
 * @param {Rule} rule
 * @returns {SeqRule}
 */
function ampSep1(rule) {
  return seq(rule, repeat(seq(token('&'), rule)));
}
