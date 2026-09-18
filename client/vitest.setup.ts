import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Globals are off, so testing-library's own cleanup never registers.
afterEach(cleanup);

// jsdom has no showModal or close on <dialog>; the guard drops this once
// it does.
if (
  typeof HTMLDialogElement !== "undefined" &&
  typeof HTMLDialogElement.prototype.showModal !== "function"
) {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.show = HTMLDialogElement.prototype.showModal;
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  };
}
