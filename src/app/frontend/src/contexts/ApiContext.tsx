import { createContext, type ReactNode, useContext, useMemo } from "react";
import { TamossApiClient } from "@/api/client";
import { config } from "@/config";

const ApiContext = createContext<TamossApiClient | null>(null);

export function ApiProvider({ children }: { children: ReactNode }) {
  const client = useMemo(() => new TamossApiClient(config.apiUrl), []);

  return <ApiContext.Provider value={client}>{children}</ApiContext.Provider>;
}

export function useApi(): TamossApiClient {
  const context = useContext(ApiContext);
  if (!context) {
    throw new Error("useApi must be used within an ApiProvider");
  }
  return context;
}
