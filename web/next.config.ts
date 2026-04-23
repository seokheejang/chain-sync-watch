import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // standalone output bundles only the runtime files needed to
  // serve the app (minimal node_modules + server.js), which shrinks
  // the Docker image by >90 % compared to copying the full
  // .next directory. The web Dockerfile copies .next/standalone +
  // .next/static + public into the runtime layer.
  output: "standalone",
};

export default nextConfig;
