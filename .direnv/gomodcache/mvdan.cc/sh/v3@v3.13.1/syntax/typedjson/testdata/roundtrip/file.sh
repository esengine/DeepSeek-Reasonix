foo
! foo
foo &
'foo' "bar"
${foo} $(bar) $((baz))
@(foo) {bar,baz}

foo && bar || baz
foo | bar |& baz

if foo; then bar; fi
for i in 1 2 3; do bar; done
for ((i = 0; i < 3; i++)); do bar; done
while foo; do bar; done
case i in foo) bar ;; esac

{ foo; }
(foo)
foo() { bar; }
declare foo
let foo=(bar)+3
time foo
coproc foo
((2))

[[ ! (foo && bar) ]]

# comment

<()
