import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  stages: [
    { duration: "15s", target: 500 }, // Ramp up to previous peak
    { duration: "40s", target: 1500 }, // Melt the server (1500 VUs)
    { duration: "15s", target: 0 }, // Ramp down
  ],
};

const BASE_URL = "https://canvas.x1nx3r.dev";
const COOKIE =
  "session=eyJhbGciOiJSUzI1NiIsImtpZCI6IjRVUXdHZyJ9.eyJpc3MiOiJodHRwczovL3Nlc3Npb24uZmlyZWJhc2UuZ29vZ2xlLmNvbS9jYW52YXMtNzg4MDIiLCJuYW1lIjoiTXVoYW1tYWQgTWVnYSBOdWdyYWhhIiwicGljdHVyZSI6Imh0dHBzOi8vbGgzLmdvb2dsZXVzZXJjb250ZW50LmNvbS9hL0FDZzhvY0pzNE5BVVl6LXNybFJzdDROLWgwVlBoUXg0eGlwY2VDTVdOMHQ2ZTBYNTBnVVRTZ1x1MDAzZHM5Ni1jIiwiYXVkIjoiY2FudmFzLTc4ODAyIiwiYXV0aF90aW1lIjoxNzgzNjgwNzI4LCJ1c2VyX2lkIjoiWUNrbHQ5UHc4cVBhSGRwNEdaZ2sxZTVDNDM1MyIsInN1YiI6IllDa2x0OVB3OHFQYUhkcDRHWmdrMWU1QzQzNTMiLCJpYXQiOjE3ODM2ODA3MzAsImV4cCI6MTc4NDg5MDMzMCwiZW1haWwiOiJtdWhhbW1hZG1lZ2FudWdyYWhhQGdtYWlsLmNvbSIsImVtYWlsX3ZlcmlmaWVkIjp0cnVlLCJmaXJlYmFzZSI6eyJpZGVudGl0aWVzIjp7Imdvb2dsZS5jb20iOlsiMTEyNzg0NjkwNTc5MjkxNjE0NDMyIl0sImVtYWlsIjpbIm11aGFtbWFkbWVnYW51Z3JhaGFAZ21haWwuY29tIl19LCJzaWduX2luX3Byb3ZpZGVyIjoiZ29vZ2xlLmNvbSJ9fQ.JKzkr7FMkCwg3Qv9Yzq_vz5Eg42HEjjXVw30HTdjsUj2T3aeahaIKdEwE906AUDE5Twlhb0yL4B6lBLKNpy1EXo4GlTcRt8OcIVrQyY4fhlnr3ARZFbJNfk9Pxmi3K9b9AFzBCDVRMDeyk_eVEknQClYpa64WYuownYs7-H-n8icXUuC_o5TDoMa2LklipghXbBjtbuNMZeHXuIC0usIambHbqpSbWMX8gc61BmsmgknSnK9E-E6JEIQg5vqMqHWhL62L5GX_ydMg7UdDWDCzkaVDfc4wQrNwQmFwtbE5wCWobgol9Q3eRWBdwDDvpeUXJ5-2BPeCXiIOlRm8YoRRw";

export default function () {
  const params = {
    headers: {
      Cookie: COOKIE,
      "Content-Type": "application/json",
    },
  };

  // 1. Create a new drawing
  let res = http.post(`${BASE_URL}/draw/new`, null, {
    headers: params.headers,
    redirects: 0,
  });

  // The backend returns a 302 redirect to /draw/:id
  check(res, { "create redirected": (r) => r.status === 302 });

  if (res.status !== 302) return;
  const loc = res.headers["Location"];
  const drawId = loc.split("/").pop();

  // 2. Save a payload (simulating autosave)
  const payload = JSON.stringify({
    elements: [{ type: "rectangle", x: 100, y: 100, width: 200, height: 100 }],
    appState: { viewBackgroundColor: "#ffffff" },
  });

  res = http.post(`${BASE_URL}/api/draw/${drawId}/save`, payload, params);
  check(res, { "save success": (r) => r.status === 200 });

  sleep(1);

  // 3. Load the data back
  res = http.get(`${BASE_URL}/api/draw/${drawId}/data`, params);
  check(res, { "load success": (r) => r.status === 200 });

  // 4. Delete the drawing to clean up
  res = http.del(`${BASE_URL}/api/draw/${drawId}`, null, params);
  check(res, { "delete success": (r) => r.status === 200 });

  sleep(1);
}
