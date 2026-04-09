export type User = {
  username: string
  role: string
  profilePicture: string
  email: string
}

export function getUser(): User {
  // Return user information

  // TODO: Call API - We use sample data for now
  const user = {
    username: 'TheArchons',
    profilePicture: '/sample-avatar.png', // real avatars should probably be stored in a bucket
    role: 'Software Developer',
    email: 'thearchons@utmist.ca',
  }

  return user
}

export function logout() {
  // TODO: Call API - we only log a message for now
  console.log('logout called')
}
