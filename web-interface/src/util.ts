type User = {
  username: string
  profilePicture: string
}

export function getUser(): User {
  // Return user information

  // TODO: Call API - We use sample data for now
  const user = {
    username: 'TheArchons',
    profilePicture: '/sample-avatar.png', // real avatars should probably be stored in a bucket
  }

  return user
}

export function logout() {
  // TODO: Call API - we only log a message for now
  console.log('logout called')
}
