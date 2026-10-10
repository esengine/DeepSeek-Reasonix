const list = document.querySelector("#tickets");
const status = document.querySelector("#queue-status");
const empty = document.querySelector("#empty-state");
const readError = document.querySelector("#read-error");
const title = document.querySelector("#title");
const titleError = document.querySelector("#title-error");
const addButton = document.querySelector("#add-button");
const filters = [...document.querySelectorAll("[data-filter]")];
let tickets = [];
let filter = "all";
let nextID = 1;

function visibleTickets() {
  return tickets.filter((ticket) => filter === "all" || ticket.complete === (filter === "complete"));
}

function render(message) {
  const openCount = tickets.filter((ticket) => !ticket.complete).length;
  document.querySelector("#all-count").textContent = tickets.length;
  document.querySelector("#open-count").textContent = openCount;
  document.querySelector("#complete-count").textContent = tickets.length - openCount;
  for (const button of filters) button.setAttribute("aria-pressed", String(button.dataset.filter === filter));
  const visible = visibleTickets();
  list.replaceChildren(...visible.map(ticketRow));
  empty.hidden = visible.length !== 0;
  empty.textContent = tickets.length === 0
    ? "The queue is empty. Add a ticket below to begin."
    : "No tickets match this filter. Choose another filter to see the queue.";
  status.textContent = message || `Showing ${visible.length} of ${tickets.length} tickets. ${openCount} open, ${tickets.length - openCount} complete.`;
}

function ticketRow(ticket) {
  const row = document.createElement("li");
  row.className = "ticket";
  row.dataset.id = ticket.id;
  const content = document.createElement("div");
  const meta = document.createElement("div");
  meta.className = "ticket-meta";
  const id = document.createElement("span");
  id.className = "ticket-id";
  id.textContent = ticket.id;
  const priority = document.createElement("span");
  priority.className = `priority priority-${ticket.priority}`;
  priority.textContent = `${ticket.priority} priority`;
  meta.append(id, priority);
  const heading = document.createElement("h3");
  heading.textContent = ticket.title;
  const summary = document.createElement("p");
  summary.className = "ticket-summary";
  summary.textContent = ticket.summary;
  content.append(meta, heading, summary);
  const label = document.createElement("label");
  label.className = "completion";
  const toggle = document.createElement("input");
  toggle.type = "checkbox";
  toggle.checked = ticket.complete;
  toggle.setAttribute("aria-label", `Complete ticket ${ticket.id}: ${ticket.title}`);
  const state = document.createElement("span");
  state.textContent = ticket.complete ? "Complete" : "Open";
  toggle.addEventListener("change", () => {
    const index = visibleTickets().findIndex((item) => item.id === ticket.id);
    ticket.complete = toggle.checked;
    render(`Ticket ${ticket.id} is now ${ticket.complete ? "complete" : "open"}.`);
    const rows = [...list.querySelectorAll("input")];
    const retained = list.querySelector(`[data-id="${CSS.escape(ticket.id)}"] input`);
    (retained || rows[Math.min(index, rows.length - 1)] || filters.find((button) => button.dataset.filter === filter)).focus();
  });
  label.append(toggle, state);
  row.append(content, label);
  return row;
}

async function loadTickets() {
  list.setAttribute("aria-busy", "true");
  readError.hidden = true;
  empty.hidden = true;
  status.textContent = "Reading sample tickets…";
  title.disabled = true;
  addButton.disabled = true;
  for (const button of filters) button.disabled = true;
  try {
    const response = await fetch("tickets.json", { cache: "no-store" });
    if (!response.ok) throw new Error("input-unavailable");
    tickets = await response.json();
    filter = "all";
    nextID = 1;
    render();
    title.disabled = false;
    addButton.disabled = false;
    for (const button of filters) button.disabled = false;
  } catch {
    list.replaceChildren();
    status.textContent = "Sample tickets could not be loaded.";
    readError.hidden = false;
  } finally {
    list.setAttribute("aria-busy", "false");
  }
}

for (const button of filters) button.addEventListener("click", () => {
  filter = button.dataset.filter;
  render();
});

document.querySelector("#add-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const text = title.value.trim();
  if (!text) {
    titleError.hidden = false;
    title.setAttribute("aria-invalid", "true");
    title.focus();
    return;
  }
  let id;
  do { id = `LOCAL-${nextID++}`; } while (tickets.some((ticket) => ticket.id === id));
  tickets.push({ id, title: text, summary: "Added locally for this page session.", priority: "normal", complete: false });
  title.value = "";
  titleError.hidden = true;
  title.removeAttribute("aria-invalid");
  filter = "all";
  render(`Added ticket ${id}. Showing all ${tickets.length} tickets.`);
  title.focus();
});

document.querySelector("#retry").addEventListener("click", () => loadTickets().then(() => {
  if (readError.hidden) filters[0].focus();
}));

loadTickets();
