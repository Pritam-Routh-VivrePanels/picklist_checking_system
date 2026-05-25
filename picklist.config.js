module.exports = {
  apps: [
    {
      name: "Picklist_Checking_System",
      script: "build/picklist_system.exe", // Point to the compiled binary
      env: { PORT: 9050 }
    }
  ]
};