// Test chat completions with Node.js fetch
const authToken = "84cf8e9b-d7a3-409e-a592-fef7ac351ead";
const runId = "f45c1293-d7da-4fa5-a001-d9eb8485c631";

const body = JSON.stringify({
  model: "mimo/mimo-v2.5",
  messages: [{ role: "user", content: "say hi in one word" }],
  codebuff_metadata: {
    cost_mode: "free",
    client_id: "test123abc",
    agent_id: "base2-free-mimo",
    run_id: runId,
    freebuff_instance_id: "b716ec04-8a28-4d66-9073-b71c7d972f8e"
  }
});

async function test() {
  // Test 1: Bun UA
  console.log("=== Test 1: Bun/1.3.14 UA ===");
  const resp1 = await fetch("https://www.codebuff.com/api/v1/chat/completions", {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${authToken}`,
      "Content-Type": "application/json",
      "Accept": "application/json, text/event-stream",
      "User-Agent": "Bun/1.3.14",
    },
    body
  });
  console.log("Status:", resp1.status);
  console.log("Response:", await resp1.text());

  // Test 2: No UA
  console.log("\n=== Test 2: No custom UA ===");
  const resp2 = await fetch("https://www.codebuff.com/api/v1/chat/completions", {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${authToken}`,
      "Content-Type": "application/json",
      "Accept": "application/json, text/event-stream",
    },
    body
  });
  console.log("Status:", resp2.status);
  console.log("Response:", await resp2.text());

  // Test 3: AI SDK UA
  console.log("\n=== Test 3: AI SDK UA ===");
  const resp3 = await fetch("https://www.codebuff.com/api/v1/chat/completions", {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${authToken}`,
      "Content-Type": "application/json",
      "Accept": "application/json, text/event-stream",
      "User-Agent": "ai-sdk/openai-compatible/1.0.0/codebuff ai-sdk/provider-utils/3.0.25 runtime/node.js/v22.23.2",
    },
    body
  });
  console.log("Status:", resp3.status);
  console.log("Response:", await resp3.text());
}

test().catch(console.error);
