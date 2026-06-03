import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ErrorMessage from "@/components/ErrorMessage";

describe("ErrorMessage", () => {
  it("renders an accessible retryable error", () => {
    const onRetry = vi.fn();
    render(<ErrorMessage message="Error" onRetry={onRetry} />);

    expect(screen.getByRole("alert")).toHaveTextContent("Error");
    fireEvent.click(screen.getByText("Retry"));

    expect(onRetry).toHaveBeenCalledOnce();
  });
});
