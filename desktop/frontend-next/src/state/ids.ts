// One sequence for every card the client mints. A caller that has to name the
// item it just added — to take it back when the kernel refuses it, or to join
// it to the turn the kernel names — draws from here, so two of them cannot
// collide.
let seq = 0;

export const nextId = () => `i${++seq}`;
