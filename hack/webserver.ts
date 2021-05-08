import { serve } from "https://deno.land/std@0.95.0/http/server.ts";
const s = serve({ port: 8000 });
console.log("http://localhost:8000/");

let counter = 0
for await (const req of s) {
  const body = JSON.parse(new TextDecoder().decode(await Deno.readAll(req.body)))
  // console.log(body);

  body.type = "com.hotellistat.test_exit"

  counter = counter + 1
  console.log("Count:", counter);

  // console.log(body);

  req.respond({ body: "Hello World\n" });

  setTimeout(async () => {
    try {
      await fetch("http://localhost:4000/complete", { method: "POST", body: JSON.stringify(body) })
    } catch (e) {
      console.log(e);
    }
  }, (Math.random() * 20))


}