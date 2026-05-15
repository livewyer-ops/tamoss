import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import TagList from "@/components/TagList";

describe("TagList", () => {
  it("shows 'No tags' when empty", () => {
    render(<TagList tags={{}} />);
    expect(screen.getByText("No tags")).toBeInTheDocument();
  });

  it("shows 'No tags' when undefined", () => {
    render(<TagList />);
    expect(screen.getByText("No tags")).toBeInTheDocument();
  });

  it("renders tag key-value pairs", () => {
    render(<TagList tags={{ env: "production", type: "video" }} />);
    expect(screen.getByText("env:")).toBeInTheDocument();
    expect(screen.getByText("production")).toBeInTheDocument();
    expect(screen.getByText("type:")).toBeInTheDocument();
    expect(screen.getByText("video")).toBeInTheDocument();
  });

  it("renders array values as comma-separated", () => {
    render(<TagList tags={{ formats: ["mp4", "mkv"] }} />);
    expect(screen.getByText("mp4, mkv")).toBeInTheDocument();
  });

  it("unwraps Pydantic anyOf wrapper objects", () => {
    render(
      <TagList
        tags={{
          tag_3: {
            actual_instance: "tag_3_value",
            any_of_schemas: ["List[str]", "str"],
          },
        }}
      />,
    );
    expect(screen.getByText("tag_3:")).toBeInTheDocument();
    expect(screen.getByText("tag_3_value")).toBeInTheDocument();
  });

  it("unwraps Pydantic anyOf wrapper with array actual_instance", () => {
    render(
      <TagList
        tags={{
          multi: {
            actual_instance: ["a", "b"],
            any_of_schemas: ["List[str]", "str"],
          },
        }}
      />,
    );
    expect(screen.getByText("a, b")).toBeInTheDocument();
  });
});
