import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import Footer from "./Footer";
it("displays the version injected from the release VERSION", () => {
  render(<MemoryRouter><Footer /></MemoryRouter>);
  expect(screen.getByText("v1.0.0")).toBeInTheDocument();
});
