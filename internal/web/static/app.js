// Cairn front-end glue. htmx does the network work; this adds CSRF wiring,
// board / calendar drag-and-drop, and a few small conveniences.
(function () {
  "use strict";

  function csrf() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? m.getAttribute("content") : "";
  }

  // ---- CSRF ---------------------------------------------------------
  document.body.addEventListener("htmx:configRequest", function (e) {
    e.detail.headers["X-CSRF-Token"] = csrf();
  });

  // Inject a hidden _csrf field into every non-htmx POST form on submit.
  document.addEventListener("submit", function (e) {
    var form = e.target;
    if (form.method && form.method.toLowerCase() === "post" && !form.querySelector('input[name="_csrf"]')) {
      var i = document.createElement("input");
      i.type = "hidden";
      i.name = "_csrf";
      i.value = csrf();
      form.appendChild(i);
    }
  });

  // ---- confirm-before-submit -------------------------------------
  document.addEventListener("click", function (e) {
    var el = e.target.closest("[data-confirm]");
    if (el && !window.confirm(el.getAttribute("data-confirm"))) {
      e.preventDefault();
      e.stopPropagation();
    }
  });

  // Don't let a click on a no-drag control (the tickbox) start a card drag.
  document.addEventListener("pointerdown", function (e) {
    var card = e.target.closest(".card");
    if (!card) return;
    card.draggable = !e.target.closest("[data-no-drag]");
  });

  // ---- board drag and drop -------------------------------------
  var dragSlug = null;

  document.addEventListener("dragstart", function (e) {
    var card = e.target.closest(".card");
    if (!card) return;
    dragSlug = card.dataset.slug;
    card.classList.add("dragging");
    e.dataTransfer.effectAllowed = "move";
  });

  document.addEventListener("dragend", function (e) {
    var card = e.target.closest(".card");
    if (card) card.classList.remove("dragging");
    document.querySelectorAll(".drop-hint").forEach(function (el) {
      el.classList.remove("drop-hint");
    });
    dragSlug = null;
  });

  document.addEventListener("dragover", function (e) {
    var zone = e.target.closest(".col-cards");
    if (!zone || !dragSlug) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    zone.classList.add("drop-hint");
  });

  document.addEventListener("dragleave", function (e) {
    var zone = e.target.closest(".col-cards");
    if (zone && !zone.contains(e.relatedTarget)) zone.classList.remove("drop-hint");
  });

  document.addEventListener("drop", function (e) {
    var zone = e.target.closest(".col-cards");
    if (!zone || !dragSlug) return;
    e.preventDefault();
    zone.classList.remove("drop-hint");

    var status = zone.dataset.status;
    var over = e.target.closest(".card");
    var before = "";
    if (over && over.dataset.slug !== dragSlug) before = over.dataset.slug;

    var board = document.getElementById("board");
    var project = board ? board.dataset.project || "" : "";

    window.htmx.ajax("POST", "/e/" + encodeURIComponent(dragSlug) + "/move", {
      target: "#board",
      swap: "outerHTML",
      values: { status: status, before: before, project: project },
    });
    dragSlug = null;
  });

  // ---- calendar drag: move a chip to another day --------------
  var calDrag = null;

  document.addEventListener("dragstart", function (e) {
    var chip = e.target.closest(".cal-chip[draggable=true]");
    if (!chip) return;
    calDrag = { kind: chip.dataset.kind, ref: chip.dataset.ref };
    chip.classList.add("dragging");
    e.dataTransfer.effectAllowed = "move";
  });

  document.addEventListener("dragover", function (e) {
    var cell = e.target.closest(".cal-day[data-date]");
    if (!cell || !calDrag) return;
    e.preventDefault();
    cell.classList.add("drop-hint");
  });

  document.addEventListener("dragleave", function (e) {
    var cell = e.target.closest(".cal-day[data-date]");
    if (cell && !cell.contains(e.relatedTarget)) cell.classList.remove("drop-hint");
  });

  document.addEventListener("drop", function (e) {
    var cell = e.target.closest(".cal-day[data-date]");
    if (!cell || !calDrag) return;
    e.preventDefault();
    cell.classList.remove("drop-hint");
    var date = cell.dataset.date;
    var url =
      calDrag.kind === "task"
        ? "/e/" + encodeURIComponent(calDrag.ref) + "/due"
        : "/events/" + encodeURIComponent(calDrag.ref) + "/move";
    window.htmx.ajax("POST", url, {
      target: "#calendar",
      swap: "outerHTML",
      values: { due: date, date: date, view: currentCalView() },
    });
    calDrag = null;
  });

  function currentCalView() {
    var c = document.getElementById("calendar");
    return c ? c.dataset.view || "month" : "month";
  }

  // ---- misc ---------------------------------------------------
  document.addEventListener("click", function (e) {
    document.querySelectorAll("details.usermenu[open]").forEach(function (d) {
      if (!d.contains(e.target)) d.removeAttribute("open");
    });
  });

  function grow(el) {
    el.style.height = "auto";
    el.style.height = el.scrollHeight + "px";
  }
  document.addEventListener("input", function (e) {
    if (e.target.matches(".edit-body, .review-body")) grow(e.target);
  });

  // Scroll the week/day time grid to the morning (or now) on load and after swaps.
  function scrollTimeGrid() {
    var grid = document.querySelector(".cal-timegrid");
    if (!grid) return;
    var hour = Math.max(0, Math.min(new Date().getHours() - 1, 20));
    var slot = grid.querySelector(".cal-slot");
    if (slot) grid.scrollTop = slot.offsetHeight * hour;
  }
  scrollTimeGrid();
  document.body.addEventListener("htmx:afterSwap", function (e) {
    if (e.target && e.target.id === "calendar") scrollTimeGrid();
  });
})();
