// The corpus the census's rules are exercised against, and what each member
// exists to tell apart. A run that passes on both halves of any pair is reading
// names, not causes.
//
//   F vs G    a memo that lists the state, and one that does not
//   H vs I    presence a state decides, and a child rendered unconditionally
//   P vs Q    an object rebuilt every render, and the same one memoised
//   S/T/U     a named function expression is a component body wherever it is
//             written, and naming one does not make it a component
//   Y/Z       a listener declares which action it is, and declaring one on a
//             resize is an error rather than a promotion
//   P1/P2     two usages of one generic control keep separate effects
//   V/W/X     an action's identity survives a change of input kind and of
//             place, and a staged action is mutating while its opening click
//             is not
//   T1-T10    where an endpoint's identity lives: through a wrapper, through
//             two, per verb, and never turning an unreadable endpoint into a
//             clean one
//   J1/J2/J3  the join itself: a witness beats an unknown, an unknown beats a
//             clean vote, and read-only needs every reachable axis closed
//   E1-E4     who owns the name a registration is written with: the platform
//             when nothing nearer does, a parameter when one shadows it, and
//             nobody when the receiver is unproven — and removing a listener
//             is not installing one
//   C1-C5     the cause set itself: two causes both survive, discovery order
//             does not change the set, one source under two projections is one
//             cause, two independent sources do not collapse, and the same
//             cause found twice is counted once
//   EP1-EP6   what the platform hands a handler: the binding is the event and
//             the spelling is not, parameter zero is what a source delivers,
//             an inner scope takes the name back, and neither an uncovered
//             member nor a receiver reached through the event is proven
//   O1-O8     an optional call is a call: the golden pair one character apart,
//             an unknown one stays unknown, a parameter executed optionally
//             still propagates while a stored one does not, and none of the
//             three optional member shapes proves anything about the receiver
//   M1-M5     which body a render target names: a React wrapper makes one an
//             alias of the other, a local function of that name makes nothing,
//             two wrapped bodies stay apart, and an alias of an alias resolves
//   H1        which formal props are followed is decided by the prop name, and
//             this is the case that says so
//   S1-S7     which attribute provides a prop: JSX is last-write, absence is an
//             answer, an unreadable spread to the right is not, and a
//             conditional object is the boundary this pass does not cross
//   PC1-PC9   a value is proven a Promise before `.then` on it means anything:
//             a declared return type and the fixpoint above it are the two
//             sources, an opaque call, an opaque binding, a binding typed
//             Promise and a member off the object are not, and a proven
//             continuation handed an unreadable callback is open, not clean
//
// Every report over this tree is frozen under tools/ui-census/golden; the
// invariants it breaks on purpose are declared in tools/ui-census/gate.mjs.
import { A, B, C, D, E, F, G } from "./cases";
import { H, I, J, L, M } from "./life";
import { N, O, P, Q, R } from "./cross";
import { J1, J2, J3 } from "./join";
import { S, T, U } from "./owner";
import { V, W1, X } from "./action";
import { Y, Z } from "./decl";
import { P1, P2 } from "./prim";
import { E1, E2, E3, E4 } from "./reg";
import { C1, C2, C3, C4, C5 } from "./cause";
import { EP1, EP2, EP3, EP4, EP5, EP6 } from "./param";
import { O1, O2, O3, O4, O5, O6, O7, O8 } from "./optional";
import { M1, M2, M4, M5 } from "./memo";
import { M3 } from "./memo2";
import { H1 } from "./propname";
import { S1, S2, S3, S4, S5, S6, S7 } from "./spread";
import { PC1, PC2, PC3, PC4, PC5, PC6, PC7, PC8, PC9 } from "./promise";
import * as transport from "./transport";
void transport;
import { W2 } from "./action2";

// Mirrors the product entry: the root element is rendered at module level, so
// nothing above it has a render site and its presence is not state's to change.
export const tree = (
  <div>
    <A />
    <B />
    <C />
    <D />
    <E />
    <F />
    <G />
    <H />
    <I />
    <J />
    <L />
    <M />
    <N />
    <O />
    <P />
    <Q />
    <R />
    <J1 />
    <J2 />
    <J3 />
    <S />
    <T />
    <U />
    <V />
    <W1 />
    <W2 />
    <X />
    <Y />
    <Z />
    <P1 />
    <P2 />
    <E1 />
    <E2 addEventListener={() => {}} />
    <E3 thing={{ addEventListener: () => {} }} />
    <E4 />
    <C1 />
    <C2 />
    <C3 />
    <C4 hidden={false} />
    <C5 />
    <EP1 />
    <EP2 />
    <EP3 />
    <EP4 />
    <EP5 />
    <EP6 />
    <O1 />
    <O2 />
    <O3 />
    <O4 />
    <O5 />
    <O6 />
    <O7 />
    <O8 />
    <M1 />
    <M2 />
    <M3 />
    <M4 />
    <M5 />
    <H1 />
    <S1 />
    <S2 />
    <S3 />
    <S4 />
    <S5 />
    <S6 />
    <S7 />
    <PC1 />
    <PC2 />
    <PC3 />
    <PC4 />
    <PC5 />
    <PC6 />
    <PC7 />
    <PC8 />
    <PC9 />
  </div>
);
