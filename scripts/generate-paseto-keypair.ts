#!/usr/bin/env -S deno run -A
import crypto from "node:crypto";
import { existsSync } from "node:fs";
import { abs } from "./_utils.ts";

if (existsSync(abs("tmp/paseto"))) {
  console.log("Paseto keypair already exists");
  Deno.exit(0);
}

const { publicKey, privateKey } = crypto.generateKeyPairSync("ed25519");
await Deno.mkdir(abs("tmp/paseto"), { recursive: true });
await Deno.writeTextFile(abs("tmp/paseto/private.pem"), privateKey.export({ format: "pem", type: "pkcs8" }).toString());
await Deno.writeTextFile(abs("tmp/paseto/public.pem"), publicKey.export({ format: "pem", type: "spki" }).toString());
