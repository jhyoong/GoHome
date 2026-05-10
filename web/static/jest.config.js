module.exports = {
  testEnvironment: 'jsdom',
  setupFilesAfterEnv: ['<rootDir>/test_setup.js'],
  testMatch: ['**/*_test.js'],
  moduleFileExtensions: ['js', 'html'],
  transform: {},
  testEnvironmentOptions: {
    url: 'http://localhost',
  },
};